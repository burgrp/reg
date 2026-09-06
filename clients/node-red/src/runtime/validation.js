function resolveRegisterName(configuredName, msg) {
  if (typeof configuredName === 'string' && configuredName.trim() !== '') {
    return configuredName.trim()
  }
  if (typeof msg?.topic === 'string' && msg.topic.trim() !== '') {
    return msg.topic.trim()
  }
  throw new Error('register name is required (configure name or provide msg.topic)')
}

function requireObject(value, fieldName) {
  if (value == null) return {}
  if (typeof value !== 'object' || Array.isArray(value)) {
    throw new Error(`${fieldName} must be an object`)
  }
  return value
}

function asInteger(value, fallback) {
  const n = Number(value)
  if (!Number.isFinite(n) || n <= 0) return fallback
  const integer = Math.floor(n)
  return integer > 0 ? integer : fallback
}

module.exports = {
  resolveRegisterName,
  requireObject,
  asInteger,
}
