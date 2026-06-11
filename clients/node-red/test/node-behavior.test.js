const test = require('node:test')
const assert = require('node:assert/strict')
const { createFakeRED, createEmitterSubscription } = require('./fake-red.js')

test('reg-request sends change request and forwards ack', async () => {
  let called = null
  const serverNode = {
    getClient() {
      return {
        async requestChange(name, value) {
          called = { name, value }
        },
      }
    },
  }

  const RED = createFakeRED({ cfg: serverNode })
  require('../nodes/reg-request.js')(RED)

  const Ctor = RED._registry['reg-request']
  const node = new Ctor({ server: 'cfg', name: 'temperature' })

  await new Promise((resolve, reject) => {
    node.emit('input', { payload: 24 }, (out) => {
      node.sent.push(out)
    }, (err) => {
      if (err) reject(err)
      else resolve()
    })
  })

  assert.deepEqual(called, { name: 'temperature', value: 24 })
  assert.equal(node.sent.length, 1)
  assert.equal(node.sent[0].requested, true)
  assert.equal(node.sent[0].topic, 'temperature')
})

test('reg-consume emits register updates and stops on close', () => {
  const sub = createEmitterSubscription()
  const serverNode = {
    getClient() {
      return {
        consume(name) {
          assert.equal(name, 'temperature')
          return sub
        },
      }
    },
  }

  const RED = createFakeRED({ cfg: serverNode })
  require('../nodes/reg-consume.js')(RED)

  const Ctor = RED._registry['reg-consume']
  const node = new Ctor({ server: 'cfg', name: 'temperature' })

  sub.emit('value', { value: 21.5, metadata: { unit: 'celsius' } })

  assert.equal(node.sent.length, 1)
  assert.deepEqual(node.sent[0], {
    topic: 'temperature',
    payload: 21.5,
    metadata: { unit: 'celsius' },
  })

  node.emit('close', () => {})
  assert.equal(sub.stopCalled, true)
})

test('reg-provide emits change requests and updates values from input', async () => {
  const sub = createEmitterSubscription({
    async update(value) {
      sub.lastUpdated = value
    },
  })

  let provided = null
  const serverNode = {
    getClient() {
      return {
        provide(name, value, metadata, ttl) {
          provided = { name, value, metadata, ttl }
          return sub
        },
      }
    },
  }

  const RED = createFakeRED({ cfg: serverNode })
  require('../nodes/reg-provide.js')(RED)

  const Ctor = RED._registry['reg-provide']
  const node = new Ctor({
    server: 'cfg',
    name: 'temperature',
    initialValue: '21.5',
    metadata: '{"unit":"celsius"}',
    ttl: '5s',
  })

  assert.deepEqual(provided, {
    name: 'temperature',
    value: 21.5,
    metadata: { unit: 'celsius' },
    ttl: '5s',
  })

  sub.emit('change', 25)
  assert.equal(node.sent.length, 1)
  assert.deepEqual(node.sent[0], { topic: 'temperature', payload: 25 })

  await new Promise((resolve, reject) => {
    node.emit('input', { payload: 26 }, () => {}, (err) => {
      if (err) reject(err)
      else resolve()
    })
  })

  assert.equal(sub.lastUpdated, 26)
})
