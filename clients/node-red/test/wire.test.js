const test = require('node:test')
const assert = require('node:assert/strict')
const { ConsumerClient } = require('../src/runtime/wire/consumer.js')
const { ProviderClient } = require('../src/runtime/wire/provider.js')

test('ConsumerClient getRegisters and requestChanges', async () => {
  const calls = []
  const fetchFn = async (url, opts) => {
    calls.push({ url, opts })
    if (!opts) {
      return {
        ok: true,
        status: 200,
        async json() {
          return { registers: { temperature: { value: 20, metadata: { unit: 'celsius' } } } }
        },
      }
    }

    return {
      ok: false,
      status: 202,
      async text() { return '' },
    }
  }

  const client = new ConsumerClient('http://localhost:8080', fetchFn)
  const regs = await client.getRegisters(['temperature'], '5s')

  assert.equal(regs.temperature.value, 20)
  await client.requestChanges({ temperature: 25 })
  assert.equal(calls.length, 2)
  assert.match(calls[0].url, /\/consumer\?name=temperature&wait=5s/)
  assert.equal(calls[1].opts.method, 'PUT')
})

test('ProviderClient setRegisters and getChangeRequests', async () => {
  const calls = []
  const fetchFn = async (url, opts) => {
    calls.push({ url, opts })
    if (opts?.method === 'PUT') {
      return {
        status: 204,
        ok: true,
        async text() { return '' },
      }
    }

    return {
      status: 200,
      ok: true,
      async json() {
        return { registers: { temperature: { value: 25 } } }
      },
      async text() { return '' },
    }
  }

  const client = new ProviderClient('http://localhost:8080', fetchFn)
  await client.setRegisters({ temperature: { value: 21, metadata: { unit: 'celsius' }, ttl: '5s' } })
  const requests = await client.getChangeRequests(['temperature'], '30s')

  assert.equal(requests.temperature, 25)
  assert.equal(calls.length, 2)
  assert.equal(calls[0].opts.method, 'PUT')
  assert.match(calls[1].url, /\/provider\?name=temperature&wait=30s/)
})

test('wire clients reject malformed successful responses', async () => {
  const consumer = new ConsumerClient('http://localhost:8080', async () => ({
    ok: true,
    status: 200,
    async json() { return { registers: { temp: {} } } },
  }))
  await assert.rejects(() => consumer.getRegisters(['temp']), /invalid/)

  const provider = new ProviderClient('http://localhost:8080', async () => ({
    ok: true,
    status: 200,
    async json() { return {} },
  }))
  await assert.rejects(() => provider.getChangeRequests(['temp']), /invalid/)
})

test('wire clients preserve prototype-like register names', async () => {
  const body = JSON.parse('{"registers":{"toString":{"value":1},"__proto__":{"value":2}}}')
  const response = async () => ({
    ok: true,
    status: 200,
    async json() { return body },
  })

  const consumer = await new ConsumerClient('http://localhost:8080', response).getRegisters()
  const requests = await new ProviderClient('http://localhost:8080', response).getChangeRequests()

  assert.equal(Object.hasOwn(consumer, '__proto__'), true)
  assert.equal(Object.hasOwn(requests, '__proto__'), true)
  assert.equal(consumer.toString.value, 1)
  assert.equal(requests.__proto__, 2)
})
