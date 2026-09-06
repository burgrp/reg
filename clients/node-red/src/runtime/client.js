const { EventEmitter } = require('node:events')
const { ConsumerClient } = require('./wire/consumer.js')
const { ProviderClient } = require('./wire/provider.js')
const { parseDuration, deepEqual, sleep, validatePollInterval } = require('./helpers.js')

class ConsumerSubscription extends EventEmitter {
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
    if (this.#stopped) throw new Error(`Provider for register '${this.#name}' is stopped`)
    await this.#client._updateProvider(this.#name, this, value)
  }

  stop() {
    if (this.#stopped) return
    this.#stopped = true
    this.#client._removeProvider(this.#name, this)
  }
}

class Client {
  #consumerWire
  #providerWire

  #consumerSubs = new Map()
  #consumerLastValues = new Map()
  #consumerRevisions = new Map()
  #consumerPolling = false
  #consumerAbort = null

  #consumeAllSubs = new Set()
  #consumeAllLastValues = new Map()
  #consumeAllRevision = 0
  #consumeAllPolling = false
  #consumeAllAbort = null

  #providerStates = new Map()
  #providerClosing = new Set()
  #providerPolling = false
  #providerAbort = null

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

  consume(name) {
    const sub = new ConsumerSubscription(this, name)

    if (!this.#consumerSubs.has(name)) {
      this.#consumerSubs.set(name, new Set())
    }
    this.#consumerSubs.get(name).add(sub)

    if (!this.#consumerAbort || this.#consumerAbort.signal.aborted) {
      this.#consumerAbort = new AbortController()
    }

    const revision = this.#consumerRevisions.get(name) ?? 0
    this._fetchInitial(name, sub, this.#consumerAbort.signal, revision)
    this.#ensureConsumerPolling()

    return sub
  }

  consumeAll() {
    const sub = new ConsumeAllSubscription(this)
    this.#consumeAllSubs.add(sub)

    if (!this.#consumeAllAbort || this.#consumeAllAbort.signal.aborted) {
      this.#consumeAllAbort = new AbortController()
    }

    this._fetchAllInitial(sub, this.#consumeAllAbort.signal, this.#consumeAllRevision)
    this.#ensureConsumeAllPolling()

    return sub
  }

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

  async requestChange(name, value) {
    await this.#consumerWire.requestChanges({ [name]: value })
  }

  async _fetchInitial(name, sub, signal, revision) {
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
      // Polling loop will retry.
    }
  }

  async _fetchAllInitial(sub, signal, revision) {
    try {
      const registers = await this.#consumerWire.getRegisters([], null, signal)
      if (sub.stopped || this.#consumeAllRevision !== revision) return
      this.#consumeAllRevision++
      this.#applyConsumeAllSnapshot(registers)
    } catch {
      // Polling loop will retry.
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

  async _updateProvider(name, sub, value) {
    const state = this.#providerStates.get(name)
    if (!state || state.sub !== sub) throw new Error(`No active provider for register '${name}'`)
    state.value = value
    try {
      await this.#queueProviderWrite(state)
    } catch {
      // Refresh timer will retry.
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

  _removeConsumeAll(sub) {
    this.#consumeAllSubs.delete(sub)
    if (this.#consumeAllSubs.size === 0) {
      this.#consumeAllRevision++
      this.#consumeAllAbort?.abort()
    }
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

module.exports = {
  Client,
  ConsumerSubscription,
  ConsumeAllSubscription,
  ProviderSubscription,
}
