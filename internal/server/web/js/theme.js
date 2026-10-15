// Applies the remembered theme before first paint, so a page opened in the
// theme opposite to the system one does not flash. A classic script on
// purpose: modules are deferred and would run after the first paint.
(function () {
  try {
    var t = window.localStorage.getItem('loom.theme');
    if (t === 'light' || t === 'dark') document.documentElement.setAttribute('data-theme', t);
  } catch (e) { /* storage blocked: follow the system theme */ }
})();
