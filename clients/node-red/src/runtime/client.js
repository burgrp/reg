const { EventEmitter } = require('node:events')
const { ConsumerClient } = require('./wire/consumer.js')
const { ProviderClient } = require('./wire/provider.js')
const { parseDuration, deepEqual, sleep } = require('./helpers.js')

class ConsumerSubscription extends EventEmitter {
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

  async request(value) {
    await this.#client.requestChange(this.#name, value)
  }

  stop() {
    if (this.#stopped) return
    this.#stopped = true
    this.#client._removeConsumer(this.#name, this)
  }
}

class ConsumeAllSubscription extends EventEmitter {
  #client
  #stopped = false

  constructor(client) {
    super()
    this.#client = client
  }

  get stopped() { return this.#stopped }

  async request(name, value) {
    await this.#client.requestChange(name, value)
  }

  stop() {
    if (this.#stopped) return
    this.#stopped = true
    this.#client._removeConsumeAll(this)
  }
}

class ProviderSubscription extends EventEmitter {
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

  async update(value) {
    await this.#client._updateProvider(this.#name, value)
  }

  stop() {
    if (this.#stopped) return
    this.#stopped = true
    this.#client._removeProvider(this.#name)
  }
}

class Client {
  #consumerWire
  #providerWire

  #consumerSubs = new Map()
  #consumerLastValues = new Map()
  #consumerPolling = false
  #consumerAbort = null

  #consumeAllSubs = new Set()
  #consumeAllLastValues = new Map()
  #consumeAllPolling = false
  #consumeAllAbort = null

  #providerStates = new Map()
  #providerPolling = false
  #providerAbort = null

  constructor(baseURL, options = {}) {
    const fetchFn = options.fetch ?? globalThis.fetch
    this.#consumerWire = new ConsumerClient(baseURL, fetchFn)
    this.#providerWire = new ProviderClient(baseURL, fetchFn)
    this.consumerPollInterval = options.consumerPollInterval ?? 5000
    this.providerPollInterval = options.providerPollInterval ?? 30000
  }

  consume(name) {
    const sub = new ConsumerSubscription(this, name)

    if (!this.#consumerSubs.has(name)) {
      this.#consumerSubs.set(name, new Set())
    }
    this.#consumerSubs.get(name).add(sub)

    if (!this.#consumerAbort) {
      this.#consumerAbort = new AbortController()
    }

    this._fetchInitial(name, sub, this.#consumerAbort.signal)
    this.#ensureConsumerPolling()

    return sub
  }

  consumeAll() {
    const sub = new ConsumeAllSubscription(this)
    this.#consumeAllSubs.add(sub)

    if (!this.#consumeAllAbort) {
      this.#consumeAllAbort = new AbortController()
    }

    this._fetchAllInitial(sub, this.#consumeAllAbort.signal)
    this.#ensureConsumeAllPolling()

    return sub
  }

  provide(name, value, metadata = {}, ttl = '5s') {
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
          // Retry on next refresh tick.
        }
      }, ttlMs / 2),
    }

    this.#providerStates.set(name, state)
    this.#ensureProviderPolling()

    return sub
  }

  async requestChange(name, value) {
    await this.#consumerWire.requestChanges({ [name]: value })
  }

  async _fetchInitial(name, sub, signal) {
    try {
      const registers = await this.#consumerWire.getRegisters([name], null, signal)
      if (name in registers && !sub.stopped) {
        const reg = registers[name]
        this.#consumerLastValues.set(name, reg)
        sub.emit('value', { value: reg.value, metadata: reg.metadata ?? {} })
      }
    } catch {
      // Polling loop will retry.
    }
  }

  async _fetchAllInitial(sub, signal) {
    try {
      const registers = await this.#consumerWire.getRegisters([], null, signal)
      if (sub.stopped) return
      for (const [name, reg] of Object.entries(registers)) {
        this.#consumeAllLastValues.set(name, reg)
        sub.emit('update', { name, value: reg.value, metadata: reg.metadata ?? {} })
      }
    } catch {
      // Polling loop will retry.
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
        continue
      }

      this.#consumerLastValues.set(name, reg)
      const update = { value: reg.value, metadata: reg.metadata ?? {} }
      for (const sub of subs) {
        if (!sub.stopped) sub.emit('value', update)
      }
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

  async _updateProvider(name, value) {
    const state = this.#providerStates.get(name)
    if (!state) throw new Error(`No active provider for register '${name}'`)
    state.value = value
    try {
      await this.#providerWire.setRegisters({
        [name]: { value, metadata: state.metadata, ttl: state.ttl },
      })
    } catch {
      // Refresh timer will retry.
    }
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

  _removeConsumeAll(sub) {
    this.#consumeAllSubs.delete(sub)
    if (this.#consumeAllSubs.size === 0) {
      this.#consumeAllAbort?.abort()
    }
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

module.exports = {
  Client,
  ConsumerSubscription,
  ConsumeAllSubscription,
  ProviderSubscription,
}
