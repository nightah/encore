/** Addresses of the servers `global-setup.ts` starts for the real-server specs. */
const port = Number(process.env.E2E_PORT ?? 19091);
const kodiPort = Number(process.env.E2E_KODI_PORT ?? 18080);

export const realServer = {
  addr: `127.0.0.1:${port}`,
  baseURL: `http://127.0.0.1:${port}/`,
  kodiAddr: `127.0.0.1:${kodiPort}`,
  kodiURL: `http://127.0.0.1:${kodiPort}/jsonrpc`,
};

/** The Echo declared in the test server's encore.yml. */
export const E2E_ECHO_ID = "amzn1.ask.device.E2ETESTECHO";

export const ADMIN = { password: "correct-horse-battery", username: "admin" };

/** Where the journey spec leaves a signed-in session for the other specs. */
export const AUTH_STATE = "e2e/.auth/admin.json";
