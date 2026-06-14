const { resolveRegisterName } = require('../src/runtime/validation.js')

module.exports = function (RED) {
  function RegRequestNode(config) {
    RED.nodes.createNode(this, config)

    const node = this
    const server = RED.nodes.getNode(config.server)
    if (!server) {
      node.status({ fill: 'red', shape: 'ring', text: 'missing registry' })
      node.error('reg-connection is required')
      return
    }

    node.status({ fill: 'blue', shape: 'ring', text: 'ready' })

    node.on('input', async (msg, send, done) => {
      try {
        const registerName = resolveRegisterName(config.name, msg)
        await server.getClient().requestChange(registerName, msg.payload)
        node.status({ fill: 'green', shape: 'dot', text: `requested ${registerName}` })
        send({
          ...msg,
          topic: registerName,
          requested: true,
        })
        done()
      } catch (err) {
        node.status({ fill: 'red', shape: 'ring', text: 'request failed' })
        done(err)
      }
    })
  }

  RED.nodes.registerType('reg-request', RegRequestNode)
}
