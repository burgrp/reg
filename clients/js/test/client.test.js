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

  const fetchFn = (url, options) =>
    new Promise((resolve, reject) => {
      const entry = { url: url.toString(), resolve, reject }
      queue.push(entry)
      // Respect AbortSignal so polling loops can exit cleanly
      const abort = () => {
        const idx = queue.indexOf(entry)
        if (idx !== -1) queue.splice(idx, 1)
        const err = new Error('The operation was aborted')
        err.name = 'AbortError'
        reject(err)
      }
      if (options?.signal?.aborted) abort()
      else options?.signal?.addEventListener('abort', abort, { once: true })
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
  const pendingURLs = () => queue.map(entry => entry.url)

  return { fetchFn, respond, respondError, pendingCount, pendingURLs }
}

/** Wait for the event loop to drain so async operations can progress */
const tick = () => new Promise(resolve => setImmediate(resolve))

describe('Client', () => {
  it('rejects invalid polling intervals', () => {
    for (const value of [0, -1, Number.NaN, Number.POSITIVE_INFINITY]) {
      assert.throws(
        () => new Client('http://localhost:8080', { consumerPollInterval: value }),
        /consumerPollInterval/
      )
      assert.throws(
        () => new Client('http://localhost:8080', { providerPollInterval: value }),
        /providerPollInterval/
      )
    }
  })

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

    it('does not emit a delayed initial value after a newer poll result', async () => {
      let resolveInitial
      let resolvePoll
      const response = body => ({
        ok: true,
        status: 200,
        json: async () => body,
        text: async () => JSON.stringify(body),
      })
      const fetchFn = (rawURL, options) => {
        const isPoll = new URL(rawURL).searchParams.has('wait')
        return new Promise((resolve, reject) => {
          if (isPoll && !resolvePoll) resolvePoll = body => resolve(response(body))
          else if (!isPoll) resolveInitial = body => resolve(response(body))
          options?.signal?.addEventListener('abort', () => {
            const error = new Error('aborted')
            error.name = 'AbortError'
            reject(error)
          }, { once: true })
        })
      }
      const client = new Client('http://localhost:8080', { fetch: fetchFn })
      const values = []
      const sub = client.consume('temp')
      sub.on('value', value => values.push(value.value))

      await tick()
      resolvePoll({ registers: { temp: { value: 2 } } })
      await tick()
      await tick()
      resolveInitial({ registers: { temp: { value: 1 } } })
      await tick()
      await tick()

      assert.deepEqual(values, [2])
      sub.stop()
    })

    it('delivers one newer value to every same-name subscriber', async () => {
      const initialResolvers = []
      let resolvePoll
      const response = body => ({
        ok: true,
        status: 200,
        json: async () => body,
        text: async () => JSON.stringify(body),
      })
      const fetchFn = (rawURL, options) => {
        const isPoll = new URL(rawURL).searchParams.has('wait')
        return new Promise((resolve, reject) => {
          if (isPoll && !resolvePoll) resolvePoll = body => resolve(response(body))
          else if (!isPoll) initialResolvers.push(body => resolve(response(body)))
          options?.signal?.addEventListener('abort', () => {
            const error = new Error('aborted')
            error.name = 'AbortError'
            reject(error)
          }, { once: true })
        })
      }
      const client = new Client('http://localhost:8080', { fetch: fetchFn })
      const firstValues = []
      const secondValues = []
      const first = client.consume('temp')
      const second = client.consume('temp')
      first.on('value', value => firstValues.push(value.value))
      second.on('value', value => secondValues.push(value.value))

      await tick()
      resolvePoll({ registers: { temp: { value: 2 } } })
      await tick()
      await tick()
      for (const resolveInitial of initialResolvers) {
        resolveInitial({ registers: { temp: { value: 1 } } })
      }
      await tick()
      await tick()

      assert.deepEqual(firstValues, [2])
      assert.deepEqual(secondValues, [2])
      first.stop()
      second.stop()
    })

    it('re-emits the same value when a missing register is recreated', async () => {
      const { fetchFn, respond } = createControllableFetch()
      const client = new Client('http://localhost:8080', {
        fetch: fetchFn,
        consumerPollInterval: 60000,
      })
      const values = []
      const sub = client.consume('temp')
      sub.on('value', value => values.push(value))

      await tick()
      respond({ registers: { temp: { value: 21.5 } } })
      await tick()
      await tick()
      respond({ registers: {} })
      await tick()
      await tick()
      respond({ registers: { temp: { value: 21.5 } } })
      await tick()
      await tick()

      assert.deepEqual(values, [
        { value: 21.5, metadata: {} },
        { value: 21.5, metadata: {} },
      ])
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

    it('keeps polling when a new subscription starts while the previous poll stops', async () => {
      const { fetchFn, pendingURLs } = createControllableFetch()
      const client = new Client('http://localhost:8080', {
        fetch: fetchFn,
        consumerPollInterval: 60000,
      })

      const first = client.consume('first')
      await tick()
      first.stop()

      const second = client.consume('second')
      await tick()
      await tick()

      assert.ok(
        pendingURLs().some(url => new URL(url).searchParams.has('wait')),
        'the replacement subscription should have an active long-poll request'
      )
      second.stop()
    })

    it('returns the change-request promise to the caller', async () => {
      const fetchFn = (_url, opts) => {
        if (opts?.method === 'PUT') {
          return Promise.resolve({ ok: false, status: 503, text: async () => 'unavailable' })
        }
        return new Promise((_, reject) => {
          opts?.signal?.addEventListener('abort', () => {
            const err = new Error('aborted'); err.name = 'AbortError'; reject(err)
          }, { once: true })
        })
      }
      const client = new Client('http://localhost:8080', { fetch: fetchFn })
      const sub = client.consume('temp')

      await assert.rejects(sub.request(25), /503/)
      sub.stop()
    })

    it('does not fabricate a missing register with a prototype-like name', async () => {
      const fetchFn = (rawURL, options) => {
        if (!new URL(rawURL).searchParams.has('wait')) {
          return Promise.resolve({
            ok: true,
            status: 200,
            json: async () => ({ registers: {} }),
            text: async () => '',
          })
        }
        return new Promise((_, reject) => {
          options?.signal?.addEventListener('abort', () => {
            const error = new Error('aborted')
            error.name = 'AbortError'
            reject(error)
          }, { once: true })
        })
      }
      const client = new Client('http://localhost:8080', { fetch: fetchFn })
      const values = []
      const sub = client.consume('toString')
      sub.on('value', value => values.push(value))

      await tick()
      await tick()

      assert.deepEqual(values, [])
      sub.stop()
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

      const pub = client.provide('temp', 21.5, { unit: 'celsius' }, '5s')

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

      const pub = client.provide('temp', 21.5, {}, '5s')
      calls.length = 0 // clear initial call

      await pub.update(25.0)

      assert.equal(calls.length, 1)
      const body = JSON.parse(calls[0].opts.body)
      assert.equal(body.registers.temp.value, 25.0)

      pub.stop()
    })

    it('returns ProviderSubscription even when initial registry set fails', async () => {
      const fetchFn = (_url, opts) => {
        if (opts?.method === 'PUT') return Promise.reject(new Error('network error'))
        // GET /provider hangs until aborted
        return new Promise((_, reject) => {
          opts?.signal?.addEventListener('abort', () => {
            const err = new Error('aborted'); err.name = 'AbortError'; reject(err)
          })
        })
      }

      const client = new Client('http://localhost:8080', { fetch: fetchFn, providerPollInterval: 60000 })
      const pub = client.provide('temp', 21.5, {}, '10s')

      assert.ok(pub)
      assert.equal(typeof pub.update, 'function')
      assert.equal(typeof pub.stop, 'function')

      pub.stop()
    })

    it('update() does not throw when registry is unavailable', async () => {
      let callCount = 0
      const fetchFn = (_url, opts) => {
        if (opts?.method === 'PUT') {
          callCount++
          if (callCount === 1) {
            // Initial provide succeeds
            return Promise.resolve({ ok: true, status: 204, json: async () => ({}), text: async () => '' })
          }
          // Subsequent updates fail (registry disconnected)
          return Promise.reject(new Error('network error'))
        }
        // GET /provider hangs until aborted
        return new Promise((_, reject) => {
          opts?.signal?.addEventListener('abort', () => {
            const err = new Error('aborted'); err.name = 'AbortError'; reject(err)
          })
        })
      }

      const client = new Client('http://localhost:8080', { fetch: fetchFn, providerPollInterval: 60000 })
      const pub = client.provide('temp', 21.5, {}, '10s')

      // update() must not throw even though the registry is down
      await assert.doesNotReject(() => pub.update(25.0))

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
      const pub = client.provide('temp', 21.5, {}, '5s')
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

    it('rejects an invalid or zero TTL before sending a request', () => {
      let calls = 0
      const client = new Client('http://localhost:8080', {
        fetch: async () => {
          calls++
          return { ok: true, status: 204, text: async () => '' }
        },
      })

      for (const ttl of ['', '0s', 'junk5s', '5s-tail', '1sBAD2m']) {
        assert.throws(() => client.provide('temp', 1, {}, ttl), /Invalid duration/)
      }
      assert.equal(calls, 0)
    })

    it('rejects a second active provider for the same register', () => {
      let putCalls = 0
      const fetchFn = (_url, opts) => {
        if (opts?.method === 'PUT') {
          putCalls++
          return Promise.resolve({ ok: true, status: 204, text: async () => '' })
        }
        return new Promise((_, reject) => {
          opts?.signal?.addEventListener('abort', () => {
            const err = new Error('aborted'); err.name = 'AbortError'; reject(err)
          }, { once: true })
        })
      }
      const client = new Client('http://localhost:8080', { fetch: fetchFn })
      const pub = client.provide('temp', 1, {}, '1h')

      assert.throws(() => client.provide('temp', 2, {}, '1h'), /already has an active provider/)
      assert.equal(putCalls, 1)
      pub.stop()
    })

    it('does not let a stopped provider update its replacement', async () => {
      const fetchFn = (_url, opts) => {
        if (opts?.method === 'PUT') {
          return Promise.resolve({ ok: true, status: 204, text: async () => '' })
        }
        return new Promise((_, reject) => {
          opts?.signal?.addEventListener('abort', () => {
            const err = new Error('aborted'); err.name = 'AbortError'; reject(err)
          }, { once: true })
        })
      }
      const client = new Client('http://localhost:8080', { fetch: fetchFn })
      const first = client.provide('temp', 1, {}, '1h')
      await tick()
      first.stop()
      await tick()
      const replacement = client.provide('temp', 2, {}, '1h')

      await assert.rejects(first.update(3), /stopped/)
      replacement.stop()
    })

    it('keeps polling when a provider starts while the previous poll stops', async () => {
      let activePolls = 0
      const fetchFn = (_url, opts) => {
        if (opts?.method === 'PUT') {
          return Promise.resolve({ ok: true, status: 204, text: async () => '' })
        }
        return new Promise((_, reject) => {
          const abort = () => {
            activePolls--
            const err = new Error('aborted'); err.name = 'AbortError'; reject(err)
          }
          if (opts?.signal?.aborted) abort()
          else {
            activePolls++
            opts?.signal?.addEventListener('abort', abort, { once: true })
          }
        })
      }
      const client = new Client('http://localhost:8080', { fetch: fetchFn })

      const first = client.provide('first', 1, {}, '1h')
      await tick()
      first.stop()
      const second = client.provide('second', 2, {}, '1h')
      await tick()
      await tick()

      assert.ok(activePolls > 0, 'the replacement provider should have an active poll')
      second.stop()
    })

    it('serializes TTL refreshes and aborts an in-flight write on stop', async () => {
      let activeWrites = 0
      let maxActiveWrites = 0
      let putCalls = 0
      const fetchFn = (_url, opts) => {
        if (opts?.method === 'PUT') {
          putCalls++
          activeWrites++
          maxActiveWrites = Math.max(maxActiveWrites, activeWrites)
          return new Promise((_, reject) => {
            opts.signal.addEventListener('abort', () => {
              activeWrites--
              const error = new Error('aborted')
              error.name = 'AbortError'
              reject(error)
            }, { once: true })
          })
        }
        return new Promise((_, reject) => {
          opts?.signal?.addEventListener('abort', () => {
            const error = new Error('aborted')
            error.name = 'AbortError'
            reject(error)
          }, { once: true })
        })
      }
      const client = new Client('http://localhost:8080', { fetch: fetchFn })
      const pub = client.provide('temp', 1, {}, '10ms')

      await new Promise(resolve => setTimeout(resolve, 35))
      assert.equal(putCalls, 1)
      assert.equal(maxActiveWrites, 1)

      pub.stop()
      await tick()
      await tick()
      assert.equal(activeWrites, 0)

      const replacement = client.provide('temp', 2, {}, '1h')
      await tick()
      assert.equal(maxActiveWrites, 1)
      replacement.stop()
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

    it('request() sends a change request for the named register', async () => {
      const putBodies = []
      const fetchFn = (_url, opts) => {
        if (opts?.method === 'PUT') {
          putBodies.push(JSON.parse(opts.body))
          return Promise.resolve({ ok: true, status: 202, json: async () => ({}), text: async () => '' })
        }
        // GET — hang until aborted
        return new Promise((_, reject) => {
          opts?.signal?.addEventListener('abort', () => {
            const err = new Error('aborted'); err.name = 'AbortError'; reject(err)
          })
        })
      }

      const client = new Client('http://localhost:8080', { fetch: fetchFn, consumerPollInterval: 60000 })
      const sub = client.consumeAll()

      sub.request('temp', 30)
      await tick()

      assert.equal(putBodies.length, 1)
      assert.equal(putBodies[0].registers.temp.value, 30)

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

    it('does not emit a delayed initial snapshot after a newer poll result', async () => {
      let resolveInitial
      let resolvePoll
      const response = body => ({
        ok: true,
        status: 200,
        json: async () => body,
        text: async () => JSON.stringify(body),
      })
      const fetchFn = (rawURL, options) => {
        const isPoll = new URL(rawURL).searchParams.has('wait')
        return new Promise((resolve, reject) => {
          if (isPoll && !resolvePoll) resolvePoll = body => resolve(response(body))
          else if (!isPoll) resolveInitial = body => resolve(response(body))
          options?.signal?.addEventListener('abort', () => {
            const error = new Error('aborted')
            error.name = 'AbortError'
            reject(error)
          }, { once: true })
        })
      }
      const client = new Client('http://localhost:8080', { fetch: fetchFn })
      const updates = []
      const sub = client.consumeAll()
      sub.on('update', update => updates.push(update.value))

      await tick()
      resolvePoll({ registers: { temp: { value: 2 } } })
      await tick()
      await tick()
      resolveInitial({ registers: { temp: { value: 1 } } })
      await tick()
      await tick()

      assert.deepEqual(updates, [2])
      sub.stop()
    })

    it('emits a removed update and forgets an expired register', async () => {
      const { fetchFn, respond } = createControllableFetch()
      const client = new Client('http://localhost:8080', {
        fetch: fetchFn,
        consumerPollInterval: 60000,
      })
      const updates = []
      const sub = client.consumeAll()
      sub.on('update', update => updates.push(update))

      await tick()
      respond({ registers: { temp: { value: 21.5 } } })
      await tick()
      await tick()
      respond({ registers: {} })
      await tick()
      await tick()

      assert.deepEqual(updates, [
        { name: 'temp', value: 21.5, metadata: {} },
        { name: 'temp', removed: true },
      ])
      sub.stop()
    })

    it('keeps polling when a new all-register subscription starts while the previous poll stops', async () => {
      const { fetchFn, pendingURLs } = createControllableFetch()
      const client = new Client('http://localhost:8080', {
        fetch: fetchFn,
        consumerPollInterval: 60000,
      })

      const first = client.consumeAll()
      await tick()
      first.stop()
      const second = client.consumeAll()
      await tick()
      await tick()

      assert.ok(
        pendingURLs().some(url => new URL(url).searchParams.has('wait')),
        'the replacement subscription should have an active long-poll request'
      )
      second.stop()
    })
  })
})
