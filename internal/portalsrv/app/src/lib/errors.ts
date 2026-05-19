/**
 * Custom error class for API errors
 */
export class ApiError extends Error {
  constructor(
    message: string,
    public code?: string,
    public statusCode?: number
  ) {
    super(message);
    this.name = "ApiError";
    Object.setPrototypeOf(this, ApiError.prototype);
  }
}

/**
 * Custom error class for WebSocket errors
 */
export class WebSocketError extends Error {
  constructor(message: string, public code?: string) {
    super(message);
    this.name = "WebSocketError";
    Object.setPrototypeOf(this, WebSocketError.prototype);
  }
}

/**
 * Custom error class for authentication errors
 */
export class AuthError extends ApiError {
  constructor(message: string, code?: string) {
    super(message, code, 401);
    this.name = "AuthError";
    Object.setPrototypeOf(this, AuthError.prototype);
  }
}
