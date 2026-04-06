// Client-side logger for Jarvis UI
var ClientLogger = (function() {
  var LOG_LEVELS = { DEBUG: 0, INFO: 1, WARN: 2, ERROR: 3, NONE: 4 };
  var currentLevel = window.JARVIS_LOG_LEVEL || LOG_LEVELS.INFO;

  function formatMessage(level, module, message) {
    return '[Jarvis] [' + level + '] [' + module + '] ' + message;
  }

  return {
    debug: function(module, message, data) {
      if (currentLevel <= LOG_LEVELS.DEBUG) {
        console.debug(formatMessage('DEBUG', module, message), data || '');
      }
    },
    info: function(module, message, data) {
      if (currentLevel <= LOG_LEVELS.INFO) {
        console.info(formatMessage('INFO', module, message), data || '');
      }
    },
    warn: function(module, message, data) {
      if (currentLevel <= LOG_LEVELS.WARN) {
        console.warn(formatMessage('WARN', module, message), data || '');
      }
    },
    error: function(module, message, error) {
      if (currentLevel <= LOG_LEVELS.ERROR) {
        console.error(formatMessage('ERROR', module, message), error || '');
      }
    }
  };
})();
