function parseDuration(duration) {
  let ms = 0
  const re = /(\d+(?:\.\d+)?)(ms|s|m|h)/g
  let match
  let matched = false
  while ((match = re.exec(duration)) !== null) {
    matched = true
    const val = parseFloat(match[1])
    switch (match[2]) {
      case 'ms': ms += val; break
      case 's': ms += val * 1000; break
      case 'm': ms += val * 60 * 1000; break
      case 'h': ms += val * 60 * 60 * 1000; break
      default: break
    }
  }
  if (!matched) throw new Error(`Invalid duration: "${duration}"`)
  return ms
}

function deepEqual(a, b) {
  return JSON.stringify(a) === JSON.stringify(b)
}

function sleep(ms) {
  return new Promise(resolve => setTimeout(resolve, ms))
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
  parseOptionalJson,
}
