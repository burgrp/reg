const { describe, it } = require('node:test')
const assert = require('node:assert/strict')
const { Client } = require('../src/runtime/client.js')
const { parseDuration } = require('../src/runtime/helpers.js')

function createControllableFetch() {
  const queue = []

  const fetchFn = (url, options) => new Promise((resolve, reject) => {
    const entry = { url: url.toString(), options, resolve, reject }
    queue.push(entry)
    const abort = () => {
      const index = queue.indexOf(entry)
      if (index !== -1) queue.splice(index, 1)
      const error = new Error('aborted')
      error.name = 'AbortError'
      reject(error)
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

  return {
    fetchFn,
    respond,
    pendingCount: () => queue.length,
    pendingURLs: () => queue.map(entry => entry.url),
  }
}

const tick = () => new Promise(resolve => setImmediate(resolve))

describe('runtime Client', () => {
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
    it('emits initial and changed values without repeating an unchanged value', async () => {
      const { fetchFn, respond } = createControllableFetch()
      const client = new Client('http://localhost:8080', { fetch: fetchFn })
      const values = []
      const sub = client.consume('temp')
      sub.on('value', value => values.push(value))

      await tick()
      respond({ registers: { temp: { value: 20 } } })
      await tick()
      await tick()
      respond({ registers: { temp: { value: 20 } } })
      await tick()
      await tick()
      respond({ registers: { temp: { value: 21, metadata: { unit: 'C' } } } })
      await tick()
      await tick()

      assert.deepEqual(values, [
        { value: 20, metadata: {} },
        { value: 21, metadata: { unit: 'C' } },
      ])
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

    it('re-emits the same value after a register is missing', async () => {
      const { fetchFn, respond } = createControllableFetch()
      const client = new Client('http://localhost:8080', { fetch: fetchFn })
      const values = []
      const sub = client.consume('temp')
      sub.on('value', value => values.push(value))

      await tick()
      respond({ registers: { temp: { value: 20 } } })
      await tick()
      await tick()
      respond({ registers: {} })
      await tick()
      await tick()
      respond({ registers: { temp: { value: 20 } } })
      await tick()
      await tick()

      assert.deepEqual(values, [
        { value: 20, metadata: {} },
        { value: 20, metadata: {} },
      ])
      sub.stop()
    })

    it('keeps polling after an immediate stop and replacement subscription', async () => {
      const { fetchFn, pendingURLs } = createControllableFetch()
      const client = new Client('http://localhost:8080', { fetch: fetchFn })
      const first = client.consume('first')

      await tick()
      first.stop()
      const second = client.consume('second')
      await tick()
      await tick()

      assert.ok(pendingURLs().some(url => new URL(url).searchParams.has('wait')))
      second.stop()
    })

    it('propagates change-request failures', async () => {
      const fetchFn = (_url, options) => {
        if (options?.method === 'PUT') {
          return Promise.resolve({ status: 503, text: async () => 'unavailable' })
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
      const sub = client.consume('temp')

      await assert.rejects(sub.request(22), /503/)
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

  describe('consumeAll()', () => {
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

    it('emits removals and re-emits a recreated register', async () => {
      const { fetchFn, respond } = createControllableFetch()
      const client = new Client('http://localhost:8080', { fetch: fetchFn })
      const updates = []
      const sub = client.consumeAll()
      sub.on('update', update => updates.push(update))

      await tick()
      respond({ registers: { temp: { value: 20 } } })
      await tick()
      await tick()
      respond({ registers: {} })
      await tick()
      await tick()
      respond({ registers: { temp: { value: 20 } } })
      await tick()
      await tick()

      assert.deepEqual(updates, [
        { name: 'temp', value: 20, metadata: {} },
        { name: 'temp', removed: true },
        { name: 'temp', value: 20, metadata: {} },
      ])
      sub.stop()
    })

    it('keeps polling after an immediate stop and replacement subscription', async () => {
      const { fetchFn, pendingURLs } = createControllableFetch()
      const client = new Client('http://localhost:8080', { fetch: fetchFn })
      const first = client.consumeAll()

      await tick()
      first.stop()
      const second = client.consumeAll()
      await tick()
      await tick()

      assert.ok(pendingURLs().some(url => new URL(url).searchParams.has('wait')))
      second.stop()
    })
  })

  describe('provide()', () => {
    it('sets, updates, and receives change requests', async () => {
      const putBodies = []
      let resolvePoll
      const fetchFn = (_url, options) => {
        if (options?.method === 'PUT') {
          putBodies.push(JSON.parse(options.body))
          return Promise.resolve({ status: 204, text: async () => '' })
        }
        return new Promise((resolve, reject) => {
          resolvePoll = body => resolve({
            ok: true,
            status: 200,
            json: async () => body,
            text: async () => '',
          })
          options?.signal?.addEventListener('abort', () => {
            const error = new Error('aborted')
            error.name = 'AbortError'
            reject(error)
          }, { once: true })
        })
      }
      const client = new Client('http://localhost:8080', { fetch: fetchFn })
      const changes = []
      const pub = client.provide('temp', 20, { unit: 'C' }, '1h')
      pub.on('change', value => changes.push(value))

      await tick()
      resolvePoll({ registers: { temp: { value: 25 } } })
      await tick()
      await tick()
      await pub.update(21)

      assert.equal(putBodies[0].registers.temp.value, 20)
      assert.equal(putBodies.at(-1).registers.temp.value, 21)
      assert.deepEqual(changes, [25])
      pub.stop()
    })

    it('validates a positive complete TTL before sending', () => {
      let calls = 0
      const client = new Client('http://localhost:8080', {
        fetch: async () => {
          calls++
          return { status: 204, text: async () => '' }
        },
      })

      assert.equal(parseDuration('1h30m500ms'), 5400500)
      for (const ttl of ['', '0s', 'junk5s', '5s-tail', '1sBAD2m']) {
        assert.throws(() => client.provide('temp', 1, {}, ttl), /Invalid duration/)
      }
      assert.equal(calls, 0)
    })

    it('rejects a duplicate active provider', () => {
      let putCalls = 0
      const fetchFn = (_url, options) => {
        if (options?.method === 'PUT') {
          putCalls++
          return Promise.resolve({ status: 204, text: async () => '' })
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
      const pub = client.provide('temp', 1, {}, '1h')

      assert.throws(() => client.provide('temp', 2, {}, '1h'), /already has an active provider/)
      assert.equal(putCalls, 1)
      pub.stop()
    })

    it('does not let a stopped provider update its replacement', async () => {
      const fetchFn = (_url, options) => {
        if (options?.method === 'PUT') {
          return Promise.resolve({ status: 204, text: async () => '' })
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
      const first = client.provide('temp', 1, {}, '1h')
      await tick()
      first.stop()
      await tick()
      const replacement = client.provide('temp', 2, {}, '1h')

      await assert.rejects(first.update(3), /stopped/)
      replacement.stop()
    })

    it('keeps polling after an immediate stop and replacement provider', async () => {
      let activePolls = 0
      const fetchFn = (_url, options) => {
        if (options?.method === 'PUT') {
          return Promise.resolve({ status: 204, text: async () => '' })
        }
        return new Promise((_, reject) => {
          const abort = () => {
            activePolls--
            const error = new Error('aborted')
            error.name = 'AbortError'
            reject(error)
          }
          if (options?.signal?.aborted) abort()
          else {
            activePolls++
            options?.signal?.addEventListener('abort', abort, { once: true })
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

      assert.ok(activePolls > 0)
      second.stop()
    })

    it('serializes TTL refreshes and aborts an in-flight write on stop', async () => {
      let activeWrites = 0
      let maxActiveWrites = 0
      let putCalls = 0
      const fetchFn = (_url, options) => {
        if (options?.method === 'PUT') {
          putCalls++
          activeWrites++
          maxActiveWrites = Math.max(maxActiveWrites, activeWrites)
          return new Promise((_, reject) => {
            options.signal.addEventListener('abort', () => {
              activeWrites--
              const error = new Error('aborted')
              error.name = 'AbortError'
              reject(error)
            }, { once: true })
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
      const provider = client.provide('temp', 1, {}, '10ms')

      await new Promise(resolve => setTimeout(resolve, 35))
      assert.equal(putCalls, 1)
      assert.equal(maxActiveWrites, 1)

      provider.stop()
      await tick()
      await tick()
      assert.equal(activeWrites, 0)

      const replacement = client.provide('temp', 2, {}, '1h')
      await tick()
      assert.equal(maxActiveWrites, 1)
      replacement.stop()
    })
  })
})