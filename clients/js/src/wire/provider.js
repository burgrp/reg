/**
 * Wire-layer provider client for the reg registry.
 *
 * Handles raw HTTP communication with the /provider endpoint.
 * Does not provide any reactive patterns or TTL refresh - use the
 * high-level Client class for that.
 */
function parseChangeRequests(body) {
  if (body === null || typeof body !== 'object' || Array.isArray(body) ||
      !Object.hasOwn(body, 'registers') || body.registers === null ||
      typeof body.registers !== 'object' || Array.isArray(body.registers)) {
    throw new Error('GET /provider returned an invalid response')
  }
  return Object.fromEntries(Object.entries(body.registers).map(([name, register]) => {
    if (register === null || typeof register !== 'object' || Array.isArray(register) ||
        !Object.hasOwn(register, 'value')) {
      throw new Error(`GET /provider returned an invalid register '${name}'`)
    }
    return [name, register.value]
  }))
}

export class ProviderClient {
  #baseURL
  #fetch

  /**
   * @param {string} baseURL - Registry base URL (e.g. "http://localhost:8080")
   * @param {function} [fetchFn] - Fetch function (defaults to globalThis.fetch)
   */
  constructor(baseURL, fetchFn = globalThis.fetch) {
    this.#baseURL = baseURL.replace(/\/$/, '')
    this.#fetch = fetchFn
  }

  /**
   * Set register values (provider operation).
   *
   * PUT /provider
   *
   * @param {Object.<string, {value: any, metadata?: Object, ttl?: string}>} registers
   *   Map of register name to update. TTL is a Go duration string (e.g. "5s", "10m").
   * @returns {Promise<void>}
   */
  async setRegisters(registers, signal = undefined) {
    const url = `${this.#baseURL}/provider`
    const body = {
      registers: Object.fromEntries(
        Object.entries(registers).map(([name, reg]) => [
          name,
          {
            value: reg.value,
            ...(reg.metadata != null && { metadata: reg.metadata }),
            ...(reg.ttl != null && { ttl: reg.ttl }),
          },
        ])
      ),
    }

    const res = await this.#fetch(url, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
      signal,
    })

    if (res.status !== 204) {
      throw new Error(`PUT /provider failed: HTTP ${res.status} ${await res.text()}`)
    }
  }

  /**
   * Poll for pending consumer change requests, optionally long-polling.
   *
   * GET /provider?name=X&name=Y&wait=30s
   *
   * Each request is consumed (removed from the queue) on retrieval.
   *
   * @param {string[]} names - Register names to poll for change requests
   * @param {string|null} [wait] - Long-poll duration string (e.g. "30s"). Null for no wait.
   * @param {AbortSignal} [signal] - Abort signal to cancel the request.
   * @returns {Promise<Object.<string, any>>} Map of register name to requested value
   */
  async getChangeRequests(names, wait = null, signal = undefined) {
    const url = new URL(`${this.#baseURL}/provider`)
    for (const name of names) {
      url.searchParams.append('name', name)
    }
    if (wait != null) {
      url.searchParams.set('wait', wait)
    }

    const res = await this.#fetch(url.toString(), signal ? { signal } : undefined)
    if (!res.ok) {
      throw new Error(`GET /provider failed: HTTP ${res.status} ${await res.text()}`)
    }

    const body = await res.json()
    return parseChangeRequests(body)
  }
}
