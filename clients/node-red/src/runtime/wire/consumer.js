function parseRegisters(body) {
  if (body === null || typeof body !== 'object' || Array.isArray(body) ||
      !Object.hasOwn(body, 'registers') || body.registers === null ||
      typeof body.registers !== 'object' || Array.isArray(body.registers)) {
    throw new Error('GET /consumer returned an invalid response')
  }
  for (const [name, register] of Object.entries(body.registers)) {
    if (register === null || typeof register !== 'object' || Array.isArray(register) ||
        !Object.hasOwn(register, 'value')) {
      throw new Error(`GET /consumer returned an invalid register '${name}'`)
    }
    if (Object.hasOwn(register, 'metadata') &&
        (register.metadata === null || typeof register.metadata !== 'object' || Array.isArray(register.metadata))) {
      throw new Error(`GET /consumer returned invalid metadata for register '${name}'`)
    }
  }
  return body.registers
}

class ConsumerClient {
  #baseURL
  #fetch

  constructor(baseURL, fetchFn = globalThis.fetch) {
    this.#baseURL = String(baseURL || '').replace(/\/$/, '')
    this.#fetch = fetchFn
  }

  async getRegisters(names = [], wait = null, signal = undefined) {
    const url = new URL(`${this.#baseURL}/consumer`)
    for (const name of names) {
      url.searchParams.append('name', name)
    }
    if (wait != null) {
      url.searchParams.set('wait', wait)
    }

    const res = await this.#fetch(url.toString(), signal ? { signal } : undefined)
    if (!res.ok) {
      throw new Error(`GET /consumer failed: HTTP ${res.status} ${await res.text()}`)
    }

    const body = await res.json()
  return parseRegisters(body)
  }

  async requestChanges(changes) {
    const url = `${this.#baseURL}/consumer`
    const body = {
      registers: Object.fromEntries(
        Object.entries(changes).map(([name, value]) => [name, { value }])
      ),
    }

    const res = await this.#fetch(url, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    })

    if (res.status !== 202) {
      throw new Error(`PUT /consumer failed: HTTP ${res.status} ${await res.text()}`)
    }
  }
}

module.exports = { ConsumerClient }
