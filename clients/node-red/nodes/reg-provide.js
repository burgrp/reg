const { parseOptionalJson, parseDuration } = require('../src/runtime/helpers.js')
const { resolveRegisterName, requireObject } = require('../src/runtime/validation.js')

module.exports = function (RED) {
  function RegProvideNode(config) {
    RED.nodes.createNode(this, config)

    const node = this
    const server = RED.nodes.getNode(config.server)
    if (!server) {
      node.status({ fill: 'red', shape: 'ring', text: 'missing reg-config' })
      node.error('reg-config is required')
      return
    }

    let sub = null

    try {
      const registerName = resolveRegisterName(config.name, null)
      const initialValue = parseOptionalJson(config.initialValue, 'initialValue')
      const initialMetadata = requireObject(parseOptionalJson(config.metadata, 'metadata'), 'metadata')
      const ttl = (config.ttl || '5s').trim()
      parseDuration(ttl)

      sub = server.getClient().provide(registerName, initialValue, initialMetadata, ttl)

      sub.on('change', (requestedValue) => {
        node.status({ fill: 'yellow', shape: 'dot', text: `change ${registerName}` })
        node.send({
          topic: registerName,
          payload: requestedValue,
        })
      })

      sub.on('error', (err) => {
        node.status({ fill: 'red', shape: 'ring', text: 'error' })
        node.error(err)
      })

      node.status({ fill: 'green', shape: 'dot', text: `providing ${registerName}` })

      node.on('input', async (msg, send, done) => {
        try {
          await sub.update(msg.payload)
          node.status({ fill: 'green', shape: 'dot', text: `updated ${registerName}` })
          done()
        } catch (err) {
          node.status({ fill: 'red', shape: 'ring', text: 'update failed' })
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

  RED.nodes.registerType('reg-provide', RegProvideNode)
}
