(() => {
  const root = document.documentElement;
  let saved = null;
  try {
    saved = window.localStorage.getItem("annolux-theme");
  } catch (_) {
    saved = null;
  }
  const system = window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
  const theme = saved === "dark" || saved === "light" ? saved : system;
  root.dataset.theme = theme;
  const themeColor = document.querySelector('meta[name="theme-color"]');
  if (themeColor) {
    themeColor.content = theme === "dark" ? (themeColor.dataset.dark || "#070b0e") : (themeColor.dataset.light || "#fcfcf9");
  }
})();
