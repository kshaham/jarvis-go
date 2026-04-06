// Debounce utility for input handlers
function debounce(func, waitMs) {
  var timeoutId = null;
  return function() {
    var context = this;
    var args = arguments;
    if (timeoutId !== null) {
      clearTimeout(timeoutId);
    }
    timeoutId = setTimeout(function() {
      func.apply(context, args);
      timeoutId = null;
    }, waitMs);
  };
}

// Throttle utility for high-frequency events
function throttle(func, limitMs) {
  var lastRun = 0;
  var timeoutId = null;
  return function() {
    var context = this;
    var args = arguments;
    var now = Date.now();
    if (now - lastRun >= limitMs) {
      func.apply(context, args);
      lastRun = now;
    } else if (timeoutId === null) {
      timeoutId = setTimeout(function() {
        func.apply(context, args);
        lastRun = Date.now();
        timeoutId = null;
      }, limitMs - (now - lastRun));
    }
  };
}
