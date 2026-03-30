(function() {
  const btn = document.getElementById('theme-toggle');
  const saved = localStorage.getItem('theme');
  if (saved) document.documentElement.setAttribute('data-theme', saved);
  if (btn) btn.addEventListener('click', function() {
    const current = document.documentElement.getAttribute('data-theme');
    const next = current === 'light' ? 'dark' : 'light';
    document.documentElement.setAttribute('data-theme', next);
    localStorage.setItem('theme', next);
    btn.textContent = next === 'light' ? '[ dark ]' : '[ light ]';
  });
})();

function sortTools(by, clickedBtn) {
  var list = document.getElementById('tool-list');
  if (!list) return;
  var cards = Array.prototype.slice.call(list.querySelectorAll('.tool-card'));
  cards.sort(function(a, b) {
    if (by === 'stars') {
      return (parseInt(b.dataset.stars) || 0) - (parseInt(a.dataset.stars) || 0);
    } else if (by === 'date') {
      return b.dataset.date.localeCompare(a.dataset.date);
    } else if (by === 'name') {
      return (a.dataset.name || '').localeCompare(b.dataset.name || '');
    }
    return 0;
  });
  cards.forEach(function(c) { list.appendChild(c); });
  var btns = document.querySelectorAll('.sort-btn');
  btns.forEach(function(b) { b.classList.remove('active'); });
  if (clickedBtn) clickedBtn.classList.add('active');
}
