function parseDuration(duration) {
  if (typeof duration !== 'string' || duration.length === 0) {
    throw new Error(`Invalid duration: "${duration}"`)
  }

  let ms = 0
  let index = 0
  const re = /(\d+(?:\.\d+)?)(ms|s|m|h)/gy
  while (index < duration.length) {
    re.lastIndex = index
    const match = re.exec(duration)
    if (!match) throw new Error(`Invalid duration: "${duration}"`)
    const val = parseFloat(match[1])
    switch (match[2]) {
      case 'ms': ms += val; break
      case 's': ms += val * 1000; break
      case 'm': ms += val * 60 * 1000; break
      case 'h': ms += val * 60 * 60 * 1000; break
      default: break
    }
    index = re.lastIndex
  }
  if (!Number.isFinite(ms) || ms <= 0) throw new Error(`Invalid duration: "${duration}"`)
  return ms
}

function deepEqual(a, b) {
  return JSON.stringify(a) === JSON.stringify(b)
}

function sleep(ms) {
  return new Promise(resolve => setTimeout(resolve, ms))
}

function validatePollInterval(value, name) {
  if (typeof value !== 'number' || !Number.isFinite(value) || value <= 0) {
    throw new Error(`${name} must be a positive finite number`)
  }
  return value
}

function parseOptionalJson(raw, fieldName) {
  if (raw == null || raw === '') return undefined
  if (typeof raw === 'object') return raw
  if (typeof raw !== 'string') {
    throw new Error(`${fieldName} must be a JSON string or object`)
  }
  try {
    return JSON.parse(raw)
  } catch (err) {
    throw new Error(`${fieldName} is not valid JSON: ${err.message}`)
  }
}

module.exports = {
  parseDuration,
  deepEqual,
  sleep,
  validatePollInterval,
  parseOptionalJson,
}
