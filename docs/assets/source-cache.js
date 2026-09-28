// The theme caches the repository facts in the header (release, stars, forks) in sessionStorage
// with no expiry, and caches a failed GitHub API call as an empty object. Session restore keeps
// sessionStorage across browser restarts, so the header can show a stale release, or nothing,
// until site data is cleared. Drop the cache when it is empty or older than MAX_AGE_MS, so the
// next page load fetches fresh facts.
(function () {
  var MAX_AGE_MS = 60 * 60 * 1000;
  var SUFFIX = ".__source";
  try {
    for (var i = sessionStorage.length - 1; i >= 0; i--) {
      var key = sessionStorage.key(i);
      if (!key || key.slice(-SUFFIX.length) !== SUFFIX) continue;
      var stampKey = key + ".cachedAt";
      var facts = JSON.parse(sessionStorage.getItem(key) || "null");
      var cachedAt = Number(sessionStorage.getItem(stampKey));
      if (!facts || Object.keys(facts).length === 0 || (cachedAt && Date.now() - cachedAt > MAX_AGE_MS)) {
        sessionStorage.removeItem(key);
        sessionStorage.removeItem(stampKey);
      } else if (!cachedAt) {
        // The theme writes no timestamp, so age is counted from the first load that sees the entry.
        sessionStorage.setItem(stampKey, String(Date.now()));
      }
    }
  } catch (e) {
    // Storage can be unavailable (private mode, blocked site data); the theme copes on its own.
  }
})();
