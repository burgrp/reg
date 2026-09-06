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

class ProviderClient {
  #baseURL
  #fetch

  constructor(baseURL, fetchFn = globalThis.fetch) {
    this.#baseURL = String(baseURL || '').replace(/\/$/, '')
    this.#fetch = fetchFn
  }

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
    return parseChangeRequests(body)
  }
}

module.exports = { ProviderClient }
