import { describe, it, beforeEach } from 'node:test'
import assert from 'node:assert/strict'
import { Client } from '../src/client.js'

/**
 * Build a controllable mock fetch for use in high-level client tests.
 *
 * Returns:
 *   - fetchFn: pass to Client constructor
 *   - respond(body): resolve the next pending fetch with the given JSON body
 *   - respondError(): reject the next pending fetch with a network error
 */
function createControllableFetch() {
  const queue = []

  const fetchFn = (_url, options) =>
    new Promise((resolve, reject) => {
      const entry = { resolve, reject }
      queue.push(entry)
      // Respect AbortSignal so polling loops can exit cleanly
      options?.signal?.addEventListener('abort', () => {
        const idx = queue.indexOf(entry)
        if (idx !== -1) queue.splice(idx, 1)
        const err = new Error('The operation was aborted')
        err.name = 'AbortError'
        reject(err)
      })
    })

  const respond = (body, status = 200) => {
    const entry = queue.shift()
    if (!entry) throw new Error('No pending fetch to respond to')
    entry.resolve({
      ok: status >= 200 && status < 300,
      status,
      json: async () => body,
      text: async () => JSON.stringify(body),
    })
  }

  const respondError = (message = 'network error') => {
    const entry = queue.shift()
    if (!entry) throw new Error('No pending fetch to reject')
    entry.reject(new Error(message))
  }

  const pendingCount = () => queue.length

  return { fetchFn, respond, respondError, pendingCount }
}

/** Wait for the event loop to drain so async operations can progress */
const tick = () => new Promise(resolve => setImmediate(resolve))

describe('Client', () => {
  describe('consume()', () => {
    it('returns a ConsumerSubscription immediately', () => {
      const client = new Client('http://localhost:8080', {
        // Abort-aware never-resolving mock so pending fetches clean up on stop()
        fetch: (_url, opts) => new Promise((_, reject) => {
          opts?.signal?.addEventListener('abort', () => {
            const err = new Error('aborted'); err.name = 'AbortError'; reject(err)
          })
        }),
      })
      const sub = client.consume('temp')
      assert.ok(sub)
      assert.equal(typeof sub.request, 'function')
      assert.equal(typeof sub.stop, 'function')
      assert.equal(typeof sub.on, 'function')
      sub.stop()
    })

    it('emits initial value immediately after fetch', async () => {
      const { fetchFn, respond } = createControllableFetch()
      const client = new Client('http://localhost:8080', {
        fetch: fetchFn,
        consumerPollInterval: 60000, // long interval to avoid re-polling in test
      })

      const values = []
      const sub = client.consume('temp')
      sub.on('value', v => values.push(v))

      // Let initial fetch start
      await tick()

      // Respond to the initial no-wait GET
      respond({ registers: { temp: { value: 21.5, metadata: { unit: 'celsius' } } } })
      await tick()
      await tick()

      assert.equal(values.length, 1)
      assert.equal(values[0].value, 21.5)
      assert.deepEqual(values[0].metadata, { unit: 'celsius' })

      sub.stop()
    })

    it('does not re-emit unchanged values from polling', async () => {
      const { fetchFn, respond } = createControllableFetch()
      const client = new Client('http://localhost:8080', {
        fetch: fetchFn,
        consumerPollInterval: 10,
      })

      const values = []
      const sub = client.consume('temp')
      sub.on('value', v => values.push(v))

      await tick()

      // Initial fetch
      respond({ registers: { temp: { value: 21.5 } } })
      await tick()
      await tick()

      // First poll — same value
      respond({ registers: { temp: { value: 21.5 } } })
      await tick()
      await tick()

      assert.equal(values.length, 1) // only from initial fetch

      sub.stop()
    })

    it('emits updated value when polling detects change', async () => {
      const { fetchFn, respond } = createControllableFetch()
      const client = new Client('http://localhost:8080', {
        fetch: fetchFn,
        consumerPollInterval: 10,
      })

      const values = []
      const sub = client.consume('temp')
      sub.on('value', v => values.push(v))

      await tick()

      // Initial fetch
      respond({ registers: { temp: { value: 21.5 } } })
      await tick()
      await tick()

      assert.equal(values.length, 1)
      assert.equal(values[0].value, 21.5)

      // Poll returns new value
      respond({ registers: { temp: { value: 25.0 } } })
      await tick()
      await tick()

      assert.equal(values.length, 2)
      assert.equal(values[1].value, 25.0)

      sub.stop()
    })

    it('batches multiple consume() subscriptions into one poll request', async () => {
      const urlsCalled = []
      const queue = []

      // Controllable mock that also records URLs (avoids tight microtask loop)
      const fetchFn = (url, options) => {
        urlsCalled.push(url.toString())
        return new Promise((resolve, reject) => {
          const entry = { resolve, reject }
          queue.push(entry)
          options?.signal?.addEventListener('abort', () => {
            const idx = queue.indexOf(entry)
            if (idx !== -1) queue.splice(idx, 1)
            const err = new Error('The operation was aborted')
            err.name = 'AbortError'
            reject(err)
          })
        })
      }

      const respond = (body) => {
        const entry = queue.shift()
        if (!entry) throw new Error('No pending fetch to respond to')
        entry.resolve({ ok: true, status: 200, json: async () => body, text: async () => JSON.stringify(body) })
      }

      const client = new Client('http://localhost:8080', {
        fetch: fetchFn,
        consumerPollInterval: 60000, // long interval — only the initial batch poll matters
      })

      const sub1 = client.consume('temp')
      const sub2 = client.consume('humidity')

      await tick()

      // Queue: [initial-temp (no-wait), first-batch-poll (temp only), initial-humidity (no-wait)]
      // Respond to the initial temp fetch and the first batch poll (temp-only).
      // The loop then restarts with both names, recording a batch URL with temp+humidity.
      respond({ registers: {} }) // initial temp
      respond({ registers: {} }) // first batch poll (temp only) → triggers new poll with both names
      await tick()
      await tick()

      // Find poll calls (URLs with wait param) that include both names
      const pollCalls = urlsCalled.filter(u => u.includes('wait='))
      assert.ok(pollCalls.length >= 1, 'Should have at least one poll call')

      const batchedPoll = pollCalls.find(u => {
        const url = new URL(u)
        const names = url.searchParams.getAll('name')
        return names.includes('temp') && names.includes('humidity')
      })
      assert.ok(batchedPoll, 'Should have a batch poll containing both temp and humidity')

      sub1.stop()
      sub2.stop()
    })

    it('stop() unsubscribes from updates', async () => {
      const { fetchFn, respond } = createControllableFetch()
      const client = new Client('http://localhost:8080', {
        fetch: fetchFn,
        consumerPollInterval: 10,
      })

      const values = []
      const sub = client.consume('temp')
      sub.on('value', v => values.push(v))

      await tick()

      // Initial fetch
      respond({ registers: { temp: { value: 1 } } })
      await tick()
      await tick()

      sub.stop()

      // Subsequent poll
      try { respond({ registers: { temp: { value: 2 } } }) } catch { /* queue may be empty */ }
      await tick()
      await tick()

      assert.equal(values.length, 1) // stopped before second value
    })
  })

  describe('provide()', () => {
    it('sets register immediately and returns ProviderSubscription', async () => {
      const calls = []
      // PUT resolves immediately; GET /provider blocks until aborted (prevents tight polling loop)
      const fetchFn = (url, opts) => {
        calls.push({ url: url.toString(), opts })
        if (opts?.method === 'PUT') {
          return Promise.resolve({ ok: true, status: 204, json: async () => ({}), text: async () => '' })
        }
        return new Promise((_, reject) => {
          opts?.signal?.addEventListener('abort', () => {
            const err = new Error('aborted'); err.name = 'AbortError'; reject(err)
          })
        })
      }

      const client = new Client('http://localhost:8080', {
        fetch: fetchFn,
        providerPollInterval: 60000,
      })

      const pub = await client.provide('temp', 21.5, { unit: 'celsius' }, '5s')

      assert.ok(pub)
      assert.equal(typeof pub.update, 'function')
      assert.equal(typeof pub.stop, 'function')
      assert.equal(typeof pub.on, 'function')

      // First call should be the initial SET
      assert.ok(calls.length >= 1)
      assert.equal(calls[0].opts.method, 'PUT')
      const body = JSON.parse(calls[0].opts.body)
      assert.deepEqual(body.registers.temp.value, 21.5)
      assert.deepEqual(body.registers.temp.ttl, '5s')

      pub.stop()
    })

    it('update() sends new value to server', async () => {
      const calls = []
      // PUT resolves immediately; GET /provider blocks until aborted (prevents tight polling loop)
      const fetchFn = (url, opts) => {
        calls.push({ url: url.toString(), opts })
        if (opts?.method === 'PUT') {
          return Promise.resolve({ ok: true, status: 204, json: async () => ({}), text: async () => '' })
        }
        return new Promise((_, reject) => {
          opts?.signal?.addEventListener('abort', () => {
            const err = new Error('aborted'); err.name = 'AbortError'; reject(err)
          })
        })
      }

      const client = new Client('http://localhost:8080', {
        fetch: fetchFn,
        providerPollInterval: 60000,
      })

      const pub = await client.provide('temp', 21.5, {}, '5s')
      calls.length = 0 // clear initial call

      await pub.update(25.0)

      assert.equal(calls.length, 1)
      const body = JSON.parse(calls[0].opts.body)
      assert.equal(body.registers.temp.value, 25.0)

      pub.stop()
    })

    it('emits change event when consumer requests a change', async () => {
      let resolveChangeRequest
      let callIndex = 0
      const fetchFn = async (url, opts) => {
        const urlStr = url.toString()
        if (opts?.method === 'PUT') {
          return { ok: true, status: 204, json: async () => ({}), text: async () => '' }
        }
        // GET /provider - first call returns a pending change request
        if (callIndex === 0) {
          callIndex++
          return {
            ok: true, status: 200,
            json: async () => new Promise(r => { resolveChangeRequest = r }),
            text: async () => '',
          }
        }
        // Subsequent calls hang to stop polling loop
        return new Promise(() => {})
      }

      const client = new Client('http://localhost:8080', {
        fetch: fetchFn,
        providerPollInterval: 10,
      })

      const changes = []
      const pub = await client.provide('temp', 21.5, {}, '5s')
      pub.on('change', v => changes.push(v))

      // Let polling loop start
      await tick()
      await tick()

      // Deliver a change request from consumer
      resolveChangeRequest({ registers: { temp: { value: 99 } } })
      await tick()
      await tick()

      assert.equal(changes.length, 1)
      assert.equal(changes[0], 99)

      pub.stop()
    })
  })

  describe('consumeAll()', () => {
    it('emits update events for all registers on initial fetch', async () => {
      const { fetchFn, respond } = createControllableFetch()
      const client = new Client('http://localhost:8080', {
        fetch: fetchFn,
        consumerPollInterval: 60000,
      })

      const updates = []
      const sub = client.consumeAll()
      sub.on('update', u => updates.push(u))

      await tick()

      respond({
        registers: {
          temp: { value: 21.5, metadata: { unit: 'celsius' } },
          humidity: { value: 55 },
        },
      })

      await tick()
      await tick()

      assert.equal(updates.length, 2)
      const names = updates.map(u => u.name).sort()
      assert.deepEqual(names, ['humidity', 'temp'])

      sub.stop()
    })

    it('includes register name in each update event', async () => {
      const { fetchFn, respond } = createControllableFetch()
      const client = new Client('http://localhost:8080', {
        fetch: fetchFn,
        consumerPollInterval: 60000,
      })

      const updates = []
      const sub = client.consumeAll()
      sub.on('update', u => updates.push(u))

      await tick()
      respond({ registers: { temp: { value: 21.5 } } })
      await tick()
      await tick()

      assert.equal(updates[0].name, 'temp')
      assert.equal(updates[0].value, 21.5)

      sub.stop()
    })
  })
})
