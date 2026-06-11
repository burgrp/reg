const { EventEmitter } = require('node:events')

function createFakeRED(configNodes = {}) {
  const registry = {}

  const RED = {
    nodes: {
      registerType(name, ctor) {
        registry[name] = ctor
      },
      createNode(node, config) {
        const emitter = new EventEmitter()
        node._emitter = emitter
        node._config = config
        node.on = emitter.on.bind(emitter)
        node.emit = emitter.emit.bind(emitter)
        node.statusCalls = []
        node.errorCalls = []
        node.sent = []
        node.status = (value) => node.statusCalls.push(value)
        node.error = (value) => node.errorCalls.push(value)
        node.send = (value) => node.sent.push(value)
      },
      getNode(id) {
        return configNodes[id] ?? null
      },
    },
    _registry: registry,
  }

  return RED
}

function createEmitterSubscription(extra = {}) {
  const emitter = new EventEmitter()
  emitter.stopCalled = false
  emitter.stop = () => {
    emitter.stopCalled = true
  }
  emitter.request = async () => {}
  emitter.update = async () => {}
  Object.assign(emitter, extra)
  return emitter
}

module.exports = {
  createFakeRED,
  createEmitterSubscription,
}
