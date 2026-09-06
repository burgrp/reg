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
  if (typeof duration !== 'string' || duration.length === 0) {
    throw new Error(`Invalid duration: "${duration}"`)
  }

  let ms = 0
  let index = 0
  const re = /(\d+(?:\.\d+)?)(ms|s|m|h)/gy
  while (index < duration.length) {
    re.lastIndex = index
    const match = re.exec(duration)
    if (!match) throw new Error(`Invalid duration: "${duration}"`)
    const val = parseFloat(match[1])
    switch (match[2]) {
      case 'ms': ms += val; break
      case 's':  ms += val * 1000; break
      case 'm':  ms += val * 60 * 1000; break
      case 'h':  ms += val * 60 * 60 * 1000; break
    }
    index = re.lastIndex
  }
  if (!Number.isFinite(ms) || ms <= 0) throw new Error(`Invalid duration: "${duration}"`)
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

function validatePollInterval(value, name) {
  if (typeof value !== 'number' || !Number.isFinite(value) || value <= 0) {
    throw new Error(`${name} must be a positive finite number`)
  }
  return value
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
  #hasValue = false

  constructor(client, name) {
    super()
    this.#client = client
    this.#name = name
  }

  get name() { return this.#name }
  get stopped() { return this.#stopped }
  get _hasValue() { return this.#hasValue }

  _emitValue(value) {
    if (this.#stopped) return
    this.#hasValue = true
    this.emit('value', value)
  }

  /**
   * Send a change request to the provider.
   * @param {any} value - Requested new value
   * @returns {Promise<void>}
   */
  request(value) {
    return this.#client._requestChange(this.#name, value)
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
 *   'update' ({name, value, metadata, removed?}) - emitted when a register changes or expires
 *   'error'  (err)                     - emitted on unrecoverable errors
 *
 * @extends EventEmitter
 */
export class ConsumeAllSubscription extends EventEmitter {
  #client
  #stopped = false
  #lastValues = new Map()

  constructor(client) {
    super()
    this.#client = client
  }

  get stopped() { return this.#stopped }

  _hasRegister(name, register) {
    const last = this.#lastValues.get(name)
    return last != null && deepEqual(last.value, register.value) && deepEqual(last.metadata, register.metadata)
  }

  _hasRegisterName(name) {
    return this.#lastValues.has(name)
  }

  _emitUpdate(update, register = null) {
    if (this.#stopped) return
    if (update.removed) this.#lastValues.delete(update.name)
    else this.#lastValues.set(update.name, register)
    this.emit('update', update)
  }

  /**
   * Send a change request to the provider of the named register.
   * @param {string} name - Register name
   * @param {any} value - Requested new value
   * @returns {Promise<void>}
   */
  request(name, value) {
    return this.#client._requestChange(name, value)
  }

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
    if (this.#stopped) throw new Error(`Provider for register '${this.#name}' is stopped`)
    await this.#client._updateProvider(this.#name, this, value)
  }

  /**
   * Stop providing and let the register expire.
   */
  stop() {
    if (this.#stopped) return
    this.#stopped = true
    this.#client._removeProvider(this.#name, this)
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
  #consumerRevisions = new Map()  // name -> latest completed snapshot generation
  #consumerPolling = false
  #consumerAbort = null           // AbortController for in-flight poll

  // ConsumeAll state
  #consumeAllSubs = new Set()     // Set<ConsumeAllSubscription>
  #consumeAllLastValues = new Map() // name -> { value, metadata }
  #consumeAllRevision = 0
  #consumeAllPolling = false
  #consumeAllAbort = null         // AbortController for in-flight poll

  // Provider batching state
  #providerStates = new Map()     // name -> { sub, value, metadata, ttl, refreshTimer }
  #providerClosing = new Set()
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
    this.consumerPollInterval = validatePollInterval(
      options.consumerPollInterval ?? 5000,
      'consumerPollInterval'
    )
    this.providerPollInterval = validatePollInterval(
      options.providerPollInterval ?? 30000,
      'providerPollInterval'
    )
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
    if (!this.#consumerAbort || this.#consumerAbort.signal.aborted) {
      this.#consumerAbort = new AbortController()
    }

    // Fetch initial value immediately (non-blocking), using session signal
    const revision = this.#consumerRevisions.get(name) ?? 0
    this.#fetchInitial(name, sub, this.#consumerAbort.signal, revision)

    // Start shared polling loop if not already running
    this.#ensureConsumerPolling()

    return sub
  }

  async #fetchInitial(name, sub, signal, revision) {
    try {
      const registers = await this.#consumerWire.getRegisters([name], null, signal)
      if (sub.stopped || (this.#consumerRevisions.get(name) ?? 0) !== revision) return

      this.#consumerRevisions.set(name, revision + 1)
      if (!Object.hasOwn(registers, name)) {
        this.#consumerLastValues.delete(name)
        return
      }

      const reg = registers[name]
      const last = this.#consumerLastValues.get(name)
      const changed = !last || !deepEqual(reg.value, last.value) || !deepEqual(reg.metadata, last.metadata)
      this.#consumerLastValues.set(name, reg)
      const update = { value: reg.value, metadata: reg.metadata ?? {} }
      for (const candidate of this.#consumerSubs.get(name) ?? []) {
        if (changed || !candidate._hasValue) candidate._emitValue(update)
      }
    } catch {
      // Silently ignore initial fetch errors (including AbortError); polling will retry
    }
  }

  #ensureConsumerPolling() {
    if (this.#consumerPolling) return
    if (!this.#consumerAbort || this.#consumerAbort.signal.aborted) {
      this.#consumerAbort = new AbortController()
    }
    const controller = this.#consumerAbort
    this.#consumerPolling = true
    this.#runConsumerLoop(controller)
  }

  async #runConsumerLoop(controller) {
    const waitStr = `${this.consumerPollInterval / 1000}s`
    while (this.#consumerSubs.size > 0) {
      const names = [...this.#consumerSubs.keys()]
      try {
        const registers = await this.#consumerWire.getRegisters(names, waitStr, controller.signal)
        this.#distributeConsumerUpdates(registers, names)
      } catch (err) {
        if (err?.name === 'AbortError') break
        await sleep(1000)
      }
    }
    if (this.#consumerAbort === controller) this.#consumerAbort = null
    this.#consumerPolling = false
    if (this.#consumerSubs.size > 0) this.#ensureConsumerPolling()
  }

  #distributeConsumerUpdates(registers, requestedNames) {
    for (const name of requestedNames) {
      this.#consumerRevisions.set(name, (this.#consumerRevisions.get(name) ?? 0) + 1)
      if (!Object.hasOwn(registers, name)) this.#consumerLastValues.delete(name)
    }

    for (const [name, reg] of Object.entries(registers)) {
      const subs = this.#consumerSubs.get(name)
      if (!subs || subs.size === 0) continue

      const last = this.#consumerLastValues.get(name)
      const changed = !last || !deepEqual(reg.value, last.value) || !deepEqual(reg.metadata, last.metadata)

      this.#consumerLastValues.set(name, reg)
      const update = { value: reg.value, metadata: reg.metadata ?? {} }
      for (const sub of subs) {
        if (changed || !sub._hasValue) sub._emitValue(update)
      }
    }
  }

  _requestChange(name, value) {
    return this.#consumerWire.requestChanges({ [name]: value })
  }

  _removeConsumer(name, sub) {
    const subs = this.#consumerSubs.get(name)
    if (!subs) return
    subs.delete(sub)
    if (subs.size === 0) {
      this.#consumerSubs.delete(name)
      this.#consumerLastValues.delete(name)
      this.#consumerRevisions.set(name, (this.#consumerRevisions.get(name) ?? 0) + 1)
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

    if (!this.#consumeAllAbort || this.#consumeAllAbort.signal.aborted) {
      this.#consumeAllAbort = new AbortController()
    }

    // Fetch all current registers immediately using session signal
    this.#fetchAllInitial(sub, this.#consumeAllAbort.signal, this.#consumeAllRevision)

    // Start polling loop if not already running
    this.#ensureConsumeAllPolling()

    return sub
  }

  async #fetchAllInitial(sub, signal, revision) {
    try {
      const registers = await this.#consumerWire.getRegisters([], null, signal)
      if (sub.stopped || this.#consumeAllRevision !== revision) return
      this.#consumeAllRevision++
      this.#applyConsumeAllSnapshot(registers)
    } catch {
      // Silently ignore initial fetch errors (including AbortError)
    }
  }

  #ensureConsumeAllPolling() {
    if (this.#consumeAllPolling) return
    if (!this.#consumeAllAbort || this.#consumeAllAbort.signal.aborted) {
      this.#consumeAllAbort = new AbortController()
    }
    const controller = this.#consumeAllAbort
    this.#consumeAllPolling = true
    this.#runConsumeAllLoop(controller)
  }

  async #runConsumeAllLoop(controller) {
    const waitStr = `${this.consumerPollInterval / 1000}s`
    while (this.#consumeAllSubs.size > 0) {
      try {
        const registers = await this.#consumerWire.getRegisters([], waitStr, controller.signal)
        this.#consumeAllRevision++
        this.#applyConsumeAllSnapshot(registers)
      } catch (err) {
        if (err?.name === 'AbortError') break
        await sleep(1000)
      }
    }
    if (this.#consumeAllAbort === controller) this.#consumeAllAbort = null
    this.#consumeAllPolling = false
    if (this.#consumeAllSubs.size > 0) this.#ensureConsumeAllPolling()
    else this.#consumeAllLastValues.clear()
  }

  #applyConsumeAllSnapshot(registers) {
    const seen = new Set(Object.keys(registers))
    for (const [name, reg] of Object.entries(registers)) {
      const last = this.#consumeAllLastValues.get(name)
      const changed = !last || !deepEqual(reg.value, last.value) || !deepEqual(reg.metadata, last.metadata)
      this.#consumeAllLastValues.set(name, reg)
      const update = { name, value: reg.value, metadata: reg.metadata ?? {} }
      for (const sub of this.#consumeAllSubs) {
        if (changed || !sub._hasRegister(name, reg)) sub._emitUpdate(update, reg)
      }
    }
    for (const name of this.#consumeAllLastValues.keys()) {
      if (seen.has(name)) continue
      this.#consumeAllLastValues.delete(name)
      const update = { name, removed: true }
      for (const sub of this.#consumeAllSubs) {
        if (sub._hasRegisterName(name)) sub._emitUpdate(update)
      }
    }
  }

  _removeConsumeAll(sub) {
    this.#consumeAllSubs.delete(sub)
    if (this.#consumeAllSubs.size === 0) {
      this.#consumeAllRevision++
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
  * @throws {Error} If the TTL is invalid or the register already has an active provider
   */
  provide(name, value, metadata = {}, ttl = '5s') {
    const ttlMs = parseDuration(ttl)
    if (this.#providerStates.has(name) || this.#providerClosing.has(name)) {
      throw new Error(`Register '${name}' already has an active provider`)
    }

    const sub = new ProviderSubscription(this, name)

    const state = {
      sub,
      value,
      metadata,
      ttl,
      controller: new AbortController(),
      writeChain: null,
      refreshQueued: false,
      refreshTimer: null,
    }

    this.#providerStates.set(name, state)
    this.#queueProviderWrite(state).catch(() => {})
    state.refreshTimer = setInterval(() => {
      if (sub.stopped || state.refreshQueued) return
      state.refreshQueued = true
      this.#queueProviderWrite(state)
        .catch(() => {})
        .finally(() => { state.refreshQueued = false })
    }, ttlMs / 2)
    this.#ensureProviderPolling()

    return sub
  }

  async _updateProvider(name, sub, value) {
    const state = this.#providerStates.get(name)
    if (!state || state.sub !== sub) throw new Error(`No active provider for register '${name}'`)
    state.value = value
    try {
      await this.#queueProviderWrite(state)
    } catch {
      // Registry unavailable; refresh timer will send the updated value when reconnected
    }
  }

  #queueProviderWrite(state) {
    const performWrite = async () => {
      if (this.#providerStates.get(state.sub.name) !== state || state.sub.stopped) return
      await this.#providerWire.setRegisters({
        [state.sub.name]: {
          value: state.value,
          metadata: state.metadata,
          ttl: state.ttl,
        },
      }, state.controller.signal)
    }
    const write = state.writeChain
      ? state.writeChain.catch(() => {}).then(performWrite)
      : performWrite()
    state.writeChain = write
    write.finally(() => {
      if (state.writeChain === write) state.writeChain = null
    }).catch(() => {})
    return write
  }

  #ensureProviderPolling() {
    if (this.#providerPolling) return
    const controller = new AbortController()
    this.#providerAbort = controller
    this.#providerPolling = true
    this.#runProviderLoop(controller)
  }

  async #runProviderLoop(controller) {
    const waitStr = `${this.providerPollInterval / 1000}s`
    while (this.#providerStates.size > 0) {
      const names = [...this.#providerStates.keys()]
      try {
        const requests = await this.#providerWire.getChangeRequests(names, waitStr, controller.signal)
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
    if (this.#providerAbort === controller) this.#providerAbort = null
    this.#providerPolling = false
    if (this.#providerStates.size > 0) this.#ensureProviderPolling()
  }

  _removeProvider(name, sub) {
    const state = this.#providerStates.get(name)
    if (!state || state.sub !== sub) return
    clearInterval(state.refreshTimer)
    this.#providerStates.delete(name)
    state.controller.abort()
    if (state.writeChain) {
      this.#providerClosing.add(name)
      state.writeChain.catch(() => {}).finally(() => this.#providerClosing.delete(name))
    }
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
