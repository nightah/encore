/** Leaving or reloading the page, behind an object so tests can replace them: a real browser doesn't let a test stub `window.location`, and a real reload or redirect would take the test page with it. */
export const pageLocation = {
  assign(url: string) {
    window.location.assign(url);
  },
  reload() {
    window.location.reload();
  },
};
