import "@testing-library/jest-dom";
import { vi } from "vitest";

// Mock browser globals if needed
global.atob = vi.fn((str) => Buffer.from(str, "base64").toString("binary"));
global.btoa = vi.fn((str) => Buffer.from(str, "binary").toString("base64"));

// Mock fetch
global.fetch = vi.fn();
