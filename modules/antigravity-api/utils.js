const { v4: uuidv4 } = require('uuid');

function generateRequestId() {
  return `agent-${uuidv4()}`;
}

function generateToolCallId() {
  return `call_${uuidv4().replace(/-/g, '')}`;
}

module.exports = {
  generateRequestId,
  generateToolCallId,
};
