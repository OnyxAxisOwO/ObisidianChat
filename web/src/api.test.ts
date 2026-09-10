import { afterEach, expect, it, vi } from "vitest";
import { newClientID } from "./api";

afterEach(() => vi.unstubAllGlobals());

it("generates message IDs when randomUUID is unavailable on HTTP", () => {
  const getRandomValues = crypto.getRandomValues.bind(crypto);
  vi.stubGlobal("crypto", { getRandomValues });
  const first = newClientID();
  const second = newClientID();
  expect(first).toMatch(/^[a-f0-9]{32}$/);
  expect(first).not.toBe(second);
});
