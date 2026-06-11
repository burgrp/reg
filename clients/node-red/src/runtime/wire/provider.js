class ProviderClient {
  #baseURL
  #fetch

  constructor(baseURL, fetchFn = globalThis.fetch) {
    this.#baseURL = String(baseURL || '').replace(/\/$/, '')
    this.#fetch = fetchFn
  }

  async setRegisters(registers) {
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
    })

    if (res.status !== 204) {
      throw new Error(`PUT /provider failed: HTTP ${res.status} ${await res.text()}`)
    }
  }

  async getChangeRequests(names = [], wait = null, signal = undefined) {
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
    const result = {}
    for (const [name, reg] of Object.entries(body.registers ?? {})) {
      result[name] = reg.value
    }
    return result
  }
}

module.exports = { ProviderClient }
