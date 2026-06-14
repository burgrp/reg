const test = require('node:test')
const assert = require('node:assert/strict')
const { createFakeRED } = require('./fake-red.js')

test('registers all node types', () => {
  const RED = createFakeRED()

  require('../nodes/reg-connection.js')(RED)
  require('../nodes/reg-consume.js')(RED)
  require('../nodes/reg-consume-all.js')(RED)
  require('../nodes/reg-provide.js')(RED)
  require('../nodes/reg-request.js')(RED)

  assert.ok(RED._registry['reg-connection'])
  assert.ok(RED._registry['reg-consume'])
  assert.ok(RED._registry['reg-consume-all'])
  assert.ok(RED._registry['reg-provide'])
  assert.ok(RED._registry['reg-request'])
})
