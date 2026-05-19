/**
 * Application configuration
 */

// Determine WebSocket protocol based on current page protocol
const isSecure = window.location.protocol === "https:";
const wsProtocol = isSecure ? "wss:" : "ws:";

// For embedded SPA: use current host for API and WebSocket
// This allows the app to work on any domain/port
const currentHost = window.location.host;

// WebSocket configuration - uses relative path from current host
export const WS_URL = `${wsProtocol}//${currentHost}/ws`;

// API configuration - use relative URLs for the embedded SPA
export const API_BASE_URL = "";

// Environment
export const IS_PRODUCTION = import.meta.env.PROD;
export const IS_DEVELOPMENT = import.meta.env.DEV;
