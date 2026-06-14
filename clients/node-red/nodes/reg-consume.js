const { resolveRegisterName } = require('../src/runtime/validation.js')

module.exports = function (RED) {
  function RegConsumeNode(config) {
    RED.nodes.createNode(this, config)

    const node = this
    const server = RED.nodes.getNode(config.server)
    if (!server) {
      node.status({ fill: 'red', shape: 'ring', text: 'missing registry' })
      node.error('reg-connection is required')
      return
    }

    let sub = null

    try {
      const registerName = resolveRegisterName(config.name, null)
      sub = server.getClient().consume(registerName)

      sub.on('value', ({ value, metadata }) => {
        node.status({ fill: 'green', shape: 'dot', text: `value ${value}` })
        node.send({
          topic: registerName,
          payload: value,
          metadata,
        })
      })

      sub.on('error', (err) => {
        node.status({ fill: 'red', shape: 'ring', text: 'error' })
        node.error(err)
      })

      node.status({ fill: 'blue', shape: 'ring', text: `subscribing ${registerName}` })

      node.on('input', async (msg, send, done) => {
        try {
          await sub.request(msg.payload)
          node.status({ fill: 'green', shape: 'dot', text: `requested ${msg.payload}` })
          done()
        } catch (err) {
          node.status({ fill: 'red', shape: 'ring', text: 'request failed' })
          done(err)
        }
      })
    } catch (err) {
      node.status({ fill: 'red', shape: 'ring', text: 'configuration error' })
      node.error(err)
    }

    node.on('close', (done) => {
      if (sub) {
        sub.stop()
      }
      done()
    })
  }

  RED.nodes.registerType('reg-consume', RegConsumeNode)
}
