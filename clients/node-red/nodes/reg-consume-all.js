module.exports = function (RED) {
  function RegConsumeAllNode(config) {
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
      sub = server.getClient().consumeAll()

      sub.on('update', ({ name, value, metadata, removed = false }) => {
        if (removed) {
          node.send({ topic: name, removed: true })
          return
        }
        node.send({ topic: name, payload: value, metadata })
      })

      sub.on('error', (err) => {
        node.status({ fill: 'red', shape: 'ring', text: 'error' })
        node.error(err)
      })

      node.on('input', async (msg, send, done) => {
        try {
          if (typeof msg.topic !== 'string' || msg.topic.trim() === '') {
            throw new Error('msg.topic is required for reg-consume-all requests')
          }
          await sub.request(msg.topic.trim(), msg.payload)
          node.status({ fill: 'green', shape: 'dot', text: `requested ${msg.topic}` })
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

  RED.nodes.registerType('reg-consume-all', RegConsumeAllNode)
}
