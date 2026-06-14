const { Client } = require('../src/runtime/client.js')
const { asInteger } = require('../src/runtime/validation.js')

module.exports = function (RED) {
  function RegConfigNode(config) {
    RED.nodes.createNode(this, config)

    this.name = config.name
    this.registryUrl = (config.registryUrl || process.env.REGISTRY || '').trim()
    this.consumerPollInterval = asInteger(config.consumerPollInterval, 5000)
    this.providerPollInterval = asInteger(config.providerPollInterval, 30000)

    if (!this.registryUrl) {
      this.error('registry URL is required in reg-connection or REGISTRY env var')
      this.client = null
      return
    }

    this.client = new Client(this.registryUrl, {
      consumerPollInterval: this.consumerPollInterval,
      providerPollInterval: this.providerPollInterval,
    })

    this.getClient = () => {
      if (!this.client) {
        throw new Error('reg-connection client is not initialized')
      }
      return this.client
    }
  }

  // Keep the legacy type so previously saved flows continue to load.
  RED.nodes.registerType('reg-config', RegConfigNode)
  RED.nodes.registerType('reg-connection', RegConfigNode)
}
