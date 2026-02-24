import { EventEmitter } from 'node:events'
import { ConsumerClient } from './wire/consumer.js'
import { ProviderClient } from './wire/provider.js'

/**
 * Parse a Go-style duration string to milliseconds.
 * Supports: ms, s, m, h
 * @param {string} duration - e.g. "5s", "10m", "1h30m"
 * @returns {number} milliseconds
 */
function parseDuration(duration) {
  let ms = 0
  const re = /(\d+(?:\.\d+)?)(ms|s|m|h)/g
  let match
  let matched = false
  while ((match = re.exec(duration)) !== null) {
    matched = true
    const val = parseFloat(match[1])
    switch (match[2]) {
      case 'ms': ms += val; break
      case 's':  ms += val * 1000; break
      case 'm':  ms += val * 60 * 1000; break
      case 'h':  ms += val * 60 * 60 * 1000; break
    }
  }
  if (!matched) throw new Error(`Invalid duration: "${duration}"`)
  return ms
}

/**
 * Deep equality check using JSON serialization.
 * @param {any} a
 * @param {any} b
 * @returns {boolean}
 */
function deepEqual(a, b) {
  return JSON.stringify(a) === JSON.stringify(b)
}

/**
 * Sleep for a given number of milliseconds.
 * @param {number} ms
 * @returns {Promise<void>}
 */
function sleep(ms) {
  return new Promise(resolve => setTimeout(resolve, ms))
}

// ─── Subscription classes ────────────────────────────────────────────────────

/**
 * Represents a consumer subscription for a single register.
 *
 * Events:
 *   'value'  ({value, metadata}) - emitted when the register value changes
 *   'error'  (err)               - emitted on unrecoverable errors
 *
 * @extends EventEmitter
 */
export class ConsumerSubscription extends EventEmitter {
  #client
  #name
  #stopped = false

  constructor(client, name) {
    super()
    this.#client = client
    this.#name = name
  }

  get name() { return this.#name }
  get stopped() { return this.#stopped }

  /**
   * Send a change request to the provider.
   * @param {any} value - Requested new value
   */
  request(value) {
    this.#client._requestChange(this.#name, value)
  }

  /**
   * Unsubscribe and stop receiving updates.
   */
  stop() {
    if (this.#stopped) return
    this.#stopped = true
    this.#client._removeConsumer(this.#name, this)
  }
}

/**
 * Represents a subscription to all registers.
 *
 * Events:
 *   'update' ({name, value, metadata}) - emitted when any register changes
 *   'error'  (err)                     - emitted on unrecoverable errors
 *
 * @extends EventEmitter
 */
export class ConsumeAllSubscription extends EventEmitter {
  #client
  #stopped = false

  constructor(client) {
    super()
    this.#client = client
  }

  get stopped() { return this.#stopped }

  /**
   * Unsubscribe and stop receiving updates.
   */
  stop() {
    if (this.#stopped) return
    this.#stopped = true
    this.#client._removeConsumeAll(this)
  }
}

/**
 * Represents a provider subscription for a single register.
 *
 * Events:
 *   'change' (value) - emitted when a consumer requests a value change
 *   'error'  (err)   - emitted on unrecoverable errors
 *
 * @extends EventEmitter
 */
export class ProviderSubscription extends EventEmitter {
  #client
  #name
  #stopped = false

  constructor(client, name) {
    super()
    this.#client = client
    this.#name = name
  }

  get name() { return this.#name }
  get stopped() { return this.#stopped }

  /**
   * Update the register value.
   * @param {any} value - New value to publish
   * @returns {Promise<void>}
   */
  async update(value) {
    await this.#client._updateProvider(this.#name, value)
  }

  /**
   * Stop providing and let the register expire.
   */
  stop() {
    if (this.#stopped) return
    this.#stopped = true
    this.#client._removeProvider(this.#name)
  }
}

// ─── High-level client ───────────────────────────────────────────────────────

/**
 * High-level client for the reg registry.
 *
 * Provides a reactive, event-driven API built on top of the wire layer.
 * Multiple subscriptions to different registers are batched into a single
 * HTTP request per poll cycle, reducing network overhead.
 *
 * @example
 * const client = new Client('http://localhost:8080')
 *
 * // Consume a register
 * const sub = client.consume('temperature')
 * sub.on('value', ({ value, metadata }) => console.log(value))
 * sub.request(22.0)  // request change
 * sub.stop()
 *
 * // Provide a register
 * const pub = client.provide('temperature', 21.5, { unit: 'celsius' }, '5s')
 * pub.on('change', requestedValue => pub.update(requestedValue))
 * pub.stop()
 */
export class Client {
  #consumerWire
  #providerWire

  // Consumer batching state
  #consumerSubs = new Map()       // name -> Set<ConsumerSubscription>
  #consumerLastValues = new Map() // name -> { value, metadata }
  #consumerPolling = false
  #consumerAbort = null           // AbortController for in-flight poll

  // ConsumeAll state
  #consumeAllSubs = new Set()     // Set<ConsumeAllSubscription>
  #consumeAllLastValues = new Map() // name -> { value, metadata }
  #consumeAllPolling = false
  #consumeAllAbort = null         // AbortController for in-flight poll

  // Provider batching state
  #providerStates = new Map()     // name -> { sub, value, metadata, ttl, refreshTimer }
  #providerPolling = false
  #providerAbort = null           // AbortController for in-flight poll

  /**
   * @param {string} baseURL - Registry base URL (e.g. "http://localhost:8080")
   * @param {object} [options]
   * @param {function} [options.fetch] - Custom fetch function
   * @param {number} [options.consumerPollInterval=5000] - Consumer poll interval in ms
   * @param {number} [options.providerPollInterval=30000] - Provider change request poll interval in ms
   */
  constructor(baseURL, options = {}) {
    const fetchFn = options.fetch ?? globalThis.fetch
    this.#consumerWire = new ConsumerClient(baseURL, fetchFn)
    this.#providerWire = new ProviderClient(baseURL, fetchFn)
    this.consumerPollInterval = options.consumerPollInterval ?? 5000
    this.providerPollInterval = options.providerPollInterval ?? 30000
  }

  // ─── consume() ─────────────────────────────────────────────────────────────

  /**
   * Subscribe to a register.
   *
   * Immediately fetches the current value, then continuously polls for updates.
   * Multiple calls for different registers share a single poll request.
   *
   * @param {string} name - Register name
   * @returns {ConsumerSubscription}
   */
  consume(name) {
    const sub = new ConsumerSubscription(this, name)

    if (!this.#consumerSubs.has(name)) {
      this.#consumerSubs.set(name, new Set())
    }
    this.#consumerSubs.get(name).add(sub)

    // Create session abort controller before the initial fetch so it can be cancelled
    if (!this.#consumerAbort) {
      this.#consumerAbort = new AbortController()
    }

    // Fetch initial value immediately (non-blocking), using session signal
    this.#fetchInitial(name, sub, this.#consumerAbort.signal)

    // Start shared polling loop if not already running
    this.#ensureConsumerPolling()

    return sub
  }

  async #fetchInitial(name, sub, signal) {
    try {
      const registers = await this.#consumerWire.getRegisters([name], null, signal)
      if (name in registers && !sub.stopped) {
        const reg = registers[name]
        this.#consumerLastValues.set(name, reg)
        sub.emit('value', { value: reg.value, metadata: reg.metadata ?? {} })
      }
    } catch {
      // Silently ignore initial fetch errors (including AbortError); polling will retry
    }
  }

  #ensureConsumerPolling() {
    if (this.#consumerPolling) return
    this.#consumerPolling = true
    this.#runConsumerLoop()
  }

  async #runConsumerLoop() {
    const waitStr = `${this.consumerPollInterval / 1000}s`
    while (this.#consumerSubs.size > 0) {
      const signal = this.#consumerAbort?.signal
      const names = [...this.#consumerSubs.keys()]
      try {
        const registers = await this.#consumerWire.getRegisters(names, waitStr, signal)
        this.#distributeConsumerUpdates(registers)
      } catch (err) {
        if (err?.name === 'AbortError') break
        await sleep(1000)
      }
    }
    this.#consumerAbort = null
    this.#consumerPolling = false
  }

  #distributeConsumerUpdates(registers) {
    for (const [name, reg] of Object.entries(registers)) {
      const subs = this.#consumerSubs.get(name)
      if (!subs || subs.size === 0) continue

      const last = this.#consumerLastValues.get(name)
      if (last && deepEqual(reg.value, last.value) && deepEqual(reg.metadata, last.metadata)) {
        continue // No change
      }

      this.#consumerLastValues.set(name, reg)
      const update = { value: reg.value, metadata: reg.metadata ?? {} }
      for (const sub of subs) {
        if (!sub.stopped) sub.emit('value', update)
      }
    }
  }

  _requestChange(name, value) {
    this.#consumerWire.requestChanges({ [name]: value }).catch(() => {})
  }

  _removeConsumer(name, sub) {
    const subs = this.#consumerSubs.get(name)
    if (!subs) return
    subs.delete(sub)
    if (subs.size === 0) {
      this.#consumerSubs.delete(name)
      this.#consumerLastValues.delete(name)
    }
    if (this.#consumerSubs.size === 0) {
      this.#consumerAbort?.abort()
    }
  }

  // ─── consumeAll() ──────────────────────────────────────────────────────────

  /**
   * Subscribe to all registers.
   *
   * Emits an 'update' event for every register on initial fetch, then
   * continuously polls and emits for any changes.
   *
   * @returns {ConsumeAllSubscription}
   */
  consumeAll() {
    const sub = new ConsumeAllSubscription(this)
    this.#consumeAllSubs.add(sub)

    if (!this.#consumeAllAbort) {
      this.#consumeAllAbort = new AbortController()
    }

    // Fetch all current registers immediately using session signal
    this.#fetchAllInitial(sub, this.#consumeAllAbort.signal)

    // Start polling loop if not already running
    this.#ensureConsumeAllPolling()

    return sub
  }

  async #fetchAllInitial(sub, signal) {
    try {
      const registers = await this.#consumerWire.getRegisters([], null, signal)
      if (sub.stopped) return
      for (const [name, reg] of Object.entries(registers)) {
        this.#consumeAllLastValues.set(name, reg)
        sub.emit('update', { name, value: reg.value, metadata: reg.metadata ?? {} })
      }
    } catch {
      // Silently ignore initial fetch errors (including AbortError)
    }
  }

  #ensureConsumeAllPolling() {
    if (this.#consumeAllPolling) return
    this.#consumeAllPolling = true
    this.#runConsumeAllLoop()
  }

  async #runConsumeAllLoop() {
    const waitStr = `${this.consumerPollInterval / 1000}s`
    while (this.#consumeAllSubs.size > 0) {
      const signal = this.#consumeAllAbort?.signal
      try {
        const registers = await this.#consumerWire.getRegisters([], waitStr, signal)
        for (const [name, reg] of Object.entries(registers)) {
          const last = this.#consumeAllLastValues.get(name)
          if (last && deepEqual(reg.value, last.value) && deepEqual(reg.metadata, last.metadata)) {
            continue
          }
          this.#consumeAllLastValues.set(name, reg)
          const update = { name, value: reg.value, metadata: reg.metadata ?? {} }
          for (const sub of this.#consumeAllSubs) {
            if (!sub.stopped) sub.emit('update', update)
          }
        }
      } catch (err) {
        if (err?.name === 'AbortError') break
        await sleep(1000)
      }
    }
    this.#consumeAllAbort = null
    this.#consumeAllPolling = false
    this.#consumeAllLastValues.clear()
  }

  _removeConsumeAll(sub) {
    this.#consumeAllSubs.delete(sub)
    if (this.#consumeAllSubs.size === 0) {
      this.#consumeAllAbort?.abort()
    }
  }

  // ─── provide() ─────────────────────────────────────────────────────────────

  /**
   * Provide (publish) a register.
   *
   * Sets the register immediately, then continuously refreshes its TTL at
   * half the TTL interval to prevent expiration. Also polls for consumer
   * change requests.
   *
   * @param {string} name - Register name
   * @param {any} value - Initial value
   * @param {Object} [metadata={}] - Register metadata
   * @param {string} [ttl='5s'] - TTL as Go duration string (e.g. "5s", "10m")
   * @returns {ProviderSubscription}
   */
  provide(name, value, metadata = {}, ttl = '5s') {
    // Fire-and-forget initial set; errors are silently ignored (registry may be unavailable).
    // The refresh timer will retry until the registry is reachable.
    this.#providerWire.setRegisters({ [name]: { value, metadata, ttl } }).catch(() => {})

    const sub = new ProviderSubscription(this, name)
    const ttlMs = parseDuration(ttl)

    const state = {
      sub,
      value,
      metadata,
      ttl,
      refreshTimer: setInterval(async () => {
        if (sub.stopped) return
        try {
          await this.#providerWire.setRegisters({
            [name]: { value: state.value, metadata: state.metadata, ttl },
          })
        } catch {
          // Ignore refresh errors; will retry on next interval
        }
      }, ttlMs / 2),
    }

    this.#providerStates.set(name, state)
    this.#ensureProviderPolling()

    return sub
  }

  async _updateProvider(name, value) {
    const state = this.#providerStates.get(name)
    if (!state) throw new Error(`No active provider for register '${name}'`)
    state.value = value
    try {
      await this.#providerWire.setRegisters({
        [name]: { value, metadata: state.metadata, ttl: state.ttl },
      })
    } catch {
      // Registry unavailable; refresh timer will send the updated value when reconnected
    }
  }

  #ensureProviderPolling() {
    if (this.#providerPolling) return
    this.#providerPolling = true
    this.#runProviderLoop()
  }

  async #runProviderLoop() {
    const waitStr = `${this.providerPollInterval / 1000}s`
    while (this.#providerStates.size > 0) {
      this.#providerAbort = new AbortController()
      const names = [...this.#providerStates.keys()]
      try {
        const requests = await this.#providerWire.getChangeRequests(names, waitStr, this.#providerAbort.signal)
        for (const [name, requestedValue] of Object.entries(requests)) {
          const state = this.#providerStates.get(name)
          if (state && !state.sub.stopped) {
            state.sub.emit('change', requestedValue)
          }
        }
      } catch (err) {
        if (err?.name === 'AbortError') break
        await sleep(1000)
      }
    }
    this.#providerAbort = null
    this.#providerPolling = false
  }

  _removeProvider(name) {
    const state = this.#providerStates.get(name)
    if (!state) return
    clearInterval(state.refreshTimer)
    this.#providerStates.delete(name)
    if (this.#providerStates.size === 0) {
      this.#providerAbort?.abort()
    }
  }
}

/**
 * Create a Client from the REGISTRY environment variable.
 * @returns {Client}
 */
export function createClientFromEnv() {
  const url = process.env.REGISTRY
  if (!url) throw new Error('REGISTRY environment variable is not set')
  return new Client(url)
}
