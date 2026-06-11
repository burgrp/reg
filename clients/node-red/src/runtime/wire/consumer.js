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
    return body.registers ?? {}
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
