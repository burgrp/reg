import { describe, it } from 'node:test'
import assert from 'node:assert/strict'
import { ConsumerClient } from '../../src/wire/consumer.js'

/**
 * Build a mock fetch function that returns a preset response.
 * @param {number} status
 * @param {any} body - JSON body (or string for error text)
 * @returns {{ fetchFn: function, calls: Array }}
 */
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

describe('ConsumerClient', () => {
  describe('getRegisters', () => {
    it('calls GET /consumer with no names', async () => {
      const { fetchFn, calls } = mockFetch(200, {
        registers: {
          temp: { value: 21.5, metadata: { unit: 'celsius' } },
        },
      })

      const client = new ConsumerClient('http://localhost:8080', fetchFn)
      const result = await client.getRegisters()

      assert.equal(calls.length, 1)
      const url = new URL(calls[0].url)
      assert.equal(url.pathname, '/consumer')
      assert.deepEqual(url.searchParams.getAll('name'), [])
      assert.equal(url.searchParams.get('wait'), null)

      assert.deepEqual(result, {
        temp: { value: 21.5, metadata: { unit: 'celsius' } },
      })
    })

    it('calls GET /consumer with multiple names', async () => {
      const { fetchFn, calls } = mockFetch(200, { registers: {} })
      const client = new ConsumerClient('http://localhost:8080', fetchFn)

      await client.getRegisters(['temp', 'humidity'])

      const url = new URL(calls[0].url)
      assert.deepEqual(url.searchParams.getAll('name'), ['temp', 'humidity'])
    })

    it('appends wait query parameter when specified', async () => {
      const { fetchFn, calls } = mockFetch(200, { registers: {} })
      const client = new ConsumerClient('http://localhost:8080', fetchFn)

      await client.getRegisters(['temp'], '5s')

      const url = new URL(calls[0].url)
      assert.equal(url.searchParams.get('wait'), '5s')
    })

    it('rejects a response when registers is missing', async () => {
      const { fetchFn } = mockFetch(200, {})
      const client = new ConsumerClient('http://localhost:8080', fetchFn)

      await assert.rejects(() => client.getRegisters(), /invalid response/)
    })

    it('rejects malformed registers and metadata', async () => {
      for (const body of [
        { registers: [] },
        { registers: { temp: {} } },
        { registers: { temp: { value: 20, metadata: null } } },
        { registers: { temp: { value: 20, metadata: [] } } },
      ]) {
        const { fetchFn } = mockFetch(200, body)
        const client = new ConsumerClient('http://localhost:8080', fetchFn)
        await assert.rejects(() => client.getRegisters(), /invalid/)
      }
    })

    it('preserves prototype-like register names', async () => {
      const body = JSON.parse('{"registers":{"toString":{"value":1},"__proto__":{"value":2}}}')
      const { fetchFn } = mockFetch(200, body)
      const client = new ConsumerClient('http://localhost:8080', fetchFn)

      const result = await client.getRegisters()

      assert.equal(Object.hasOwn(result, 'toString'), true)
      assert.equal(Object.hasOwn(result, '__proto__'), true)
      assert.equal(result.toString.value, 1)
      assert.equal(result.__proto__.value, 2)
    })

    it('strips trailing slash from base URL', async () => {
      const { fetchFn, calls } = mockFetch(200, { registers: {} })
      const client = new ConsumerClient('http://localhost:8080/', fetchFn)

      await client.getRegisters()

      const url = new URL(calls[0].url)
      assert.equal(url.pathname, '/consumer')
    })

    it('throws on non-ok HTTP status', async () => {
      const { fetchFn } = mockFetch(500, 'internal error')
      const client = new ConsumerClient('http://localhost:8080', fetchFn)

      await assert.rejects(() => client.getRegisters(), /HTTP 500/)
    })
  })

  describe('requestChanges', () => {
    it('sends PUT /consumer with correct body', async () => {
      const { fetchFn, calls } = mockFetch(202, '')
      const client = new ConsumerClient('http://localhost:8080', fetchFn)

      await client.requestChanges({ temp: 25.0, humidity: 60 })

      assert.equal(calls.length, 1)
      assert.equal(calls[0].options.method, 'PUT')
      assert.equal(new URL(calls[0].url).pathname, '/consumer')

      const body = JSON.parse(calls[0].options.body)
      assert.deepEqual(body, {
        registers: {
          temp: { value: 25.0 },
          humidity: { value: 60 },
        },
      })
    })

    it('sets Content-Type header', async () => {
      const { fetchFn, calls } = mockFetch(202, '')
      const client = new ConsumerClient('http://localhost:8080', fetchFn)

      await client.requestChanges({ temp: 1 })

      assert.equal(calls[0].options.headers['Content-Type'], 'application/json')
    })

    it('throws when status is not 202', async () => {
      const { fetchFn } = mockFetch(400, 'bad request')
      const client = new ConsumerClient('http://localhost:8080', fetchFn)

      await assert.rejects(() => client.requestChanges({ temp: 1 }), /HTTP 400/)
    })

    it('sends null value correctly', async () => {
      const { fetchFn, calls } = mockFetch(202, '')
      const client = new ConsumerClient('http://localhost:8080', fetchFn)

      await client.requestChanges({ temp: null })

      const body = JSON.parse(calls[0].options.body)
      assert.deepEqual(body.registers.temp, { value: null })
    })
  })
})
