import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { ProviderClient } from '../../src/wire/provider.js'

function mockFetch(status, body) {
  const calls = []
  const fetchFn = async (url, options) => {
    calls.push({ url: url.toString(), options })
    const isJson = typeof body !== 'string'
    return {
      ok: status >= 200 && status < 300,
      status,
      json: async () => (isJson ? body : JSON.parse(body)),
      text: async () => (isJson ? JSON.stringify(body) : body),
    }
  }
  return { fetchFn, calls }
}

describe('ProviderClient', () => {
  describe('setRegisters', () => {
    it('sends PUT /provider with value, metadata, and ttl', async () => {
      const { fetchFn, calls } = mockFetch(204, '')
      const client = new ProviderClient('http://localhost:8080', fetchFn)

      await client.setRegisters({
        temp: { value: 21.5, metadata: { unit: 'celsius' }, ttl: '5s' },
      })

      assert.equal(calls.length, 1)
      assert.equal(calls[0].options.method, 'PUT')
      assert.equal(new URL(calls[0].url).pathname, '/provider')

      const body = JSON.parse(calls[0].options.body)
      assert.deepEqual(body, {
        registers: {
          temp: { value: 21.5, metadata: { unit: 'celsius' }, ttl: '5s' },
        },
      })
    })

    it('omits metadata and ttl when not provided', async () => {
      const { fetchFn, calls } = mockFetch(204, '')
      const client = new ProviderClient('http://localhost:8080', fetchFn)

      await client.setRegisters({ temp: { value: 42 } })

      const body = JSON.parse(calls[0].options.body)
      assert.deepEqual(body.registers.temp, { value: 42 })
    })

    it('sends multiple registers in one request', async () => {
      const { fetchFn, calls } = mockFetch(204, '')
      const client = new ProviderClient('http://localhost:8080', fetchFn)

      await client.setRegisters({
        temp: { value: 21.5, ttl: '5s' },
        humidity: { value: 55, ttl: '5s' },
      })

      const body = JSON.parse(calls[0].options.body)
      assert.ok('temp' in body.registers)
      assert.ok('humidity' in body.registers)
    })

    it('sets Content-Type header', async () => {
      const { fetchFn, calls } = mockFetch(204, '')
      const client = new ProviderClient('http://localhost:8080', fetchFn)

      await client.setRegisters({ temp: { value: 1 } })

      assert.equal(calls[0].options.headers['Content-Type'], 'application/json')
    })

    it('throws when status is not 204', async () => {
      const { fetchFn } = mockFetch(400, 'bad request')
      const client = new ProviderClient('http://localhost:8080', fetchFn)

      await assert.rejects(() => client.setRegisters({ temp: { value: 1 } }), /HTTP 400/)
    })

    it('sends boolean and null values correctly', async () => {
      const { fetchFn, calls } = mockFetch(204, '')
      const client = new ProviderClient('http://localhost:8080', fetchFn)

      await client.setRegisters({
        flag: { value: true },
        empty: { value: null },
      })

      const body = JSON.parse(calls[0].options.body)
      assert.equal(body.registers.flag.value, true)
      assert.equal(body.registers.empty.value, null)
    })
  })

  describe('getChangeRequests', () => {
    it('calls GET /provider with names and wait', async () => {
      const { fetchFn, calls } = mockFetch(200, {
        registers: { temp: { value: 25.0 } },
      })
      const client = new ProviderClient('http://localhost:8080', fetchFn)

      const result = await client.getChangeRequests(['temp'], '30s')

      assert.equal(calls.length, 1)
      const url = new URL(calls[0].url)
      assert.equal(url.pathname, '/provider')
      assert.deepEqual(url.searchParams.getAll('name'), ['temp'])
      assert.equal(url.searchParams.get('wait'), '30s')

      assert.deepEqual(result, { temp: 25.0 })
    })

    it('polls multiple registers in one request', async () => {
      const { fetchFn, calls } = mockFetch(200, { registers: {} })
      const client = new ProviderClient('http://localhost:8080', fetchFn)

      await client.getChangeRequests(['temp', 'humidity'])

      const url = new URL(calls[0].url)
      assert.deepEqual(url.searchParams.getAll('name'), ['temp', 'humidity'])
    })

    it('returns empty object when no pending requests', async () => {
      const { fetchFn } = mockFetch(200, { registers: {} })
      const client = new ProviderClient('http://localhost:8080', fetchFn)

      const result = await client.getChangeRequests(['temp'])
      assert.deepEqual(result, {})
    })

    it('extracts value from register wrapper', async () => {
      const { fetchFn } = mockFetch(200, {
        registers: {
          temp: { value: 99 },
          flag: { value: false },
        },
      })
      const client = new ProviderClient('http://localhost:8080', fetchFn)

      const result = await client.getChangeRequests(['temp', 'flag'])
      assert.deepEqual(result, { temp: 99, flag: false })
    })

    it('rejects malformed responses', async () => {
      for (const body of [
        {},
        { registers: [] },
        { registers: { temp: {} } },
      ]) {
        const { fetchFn } = mockFetch(200, body)
        const client = new ProviderClient('http://localhost:8080', fetchFn)
        await assert.rejects(() => client.getChangeRequests(['temp']), /invalid/)
      }
    })

    it('preserves prototype-like register names', async () => {
      const body = JSON.parse('{"registers":{"toString":{"value":1},"__proto__":{"value":2}}}')
      const { fetchFn } = mockFetch(200, body)
      const client = new ProviderClient('http://localhost:8080', fetchFn)

      const result = await client.getChangeRequests(['toString', '__proto__'])

      assert.equal(Object.hasOwn(result, 'toString'), true)
      assert.equal(Object.hasOwn(result, '__proto__'), true)
      assert.equal(result.toString, 1)
      assert.equal(result.__proto__, 2)
    })

    it('throws on non-ok HTTP status', async () => {
      const { fetchFn } = mockFetch(503, 'unavailable')
      const client = new ProviderClient('http://localhost:8080', fetchFn)

      await assert.rejects(() => client.getChangeRequests(['temp']), /HTTP 503/)
    })
  })
})
