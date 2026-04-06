---
phase: 06-websocket-auto-reconnection
plan: "01"
status: complete
completed: "2026-04-06"
commits:
  - "3cd0970 feat(06-01): add JWT authentication to WebSocket upgrade endpoint"
---

# Plan 06-01: JWT Authentication for WebSocket

## Summary

Successfully implemented JWT authentication for the WebSocket upgrade endpoint. The WebSocket connection now validates JWT tokens passed via URL query parameters before allowing connection establishment.

## What Was Built

### Backend Changes

1. **JWTService Interface** (`internal/api/middleware/auth.go`)
   - Defined `TokenClaims` struct with UserID, Username, TokenType, ExpiresAt
   - Created `JWTService` interface with `ValidateAccessToken` method
   - Added common error variables (ErrInvalidToken, ErrExpiredToken, ErrTokenMissing)

2. **WebSocket JWT Validation** (`internal/service/notification/websocket.go`)
   - Updated `HandleWebSocket` to accept `jwtService` parameter
   - Added token extraction from query parameter: `c.Query("token")`
   - Returns 401 Unauthorized for missing or invalid tokens
   - Uses validated user ID in clientID format for traceability

3. **NotificationHandler Updates** (`internal/api/handler/notification.go`)
   - Added `jwtService` field to `NotificationHandler` struct
   - Updated constructor to accept and store JWT service
   - Passes JWT service to `HandleWebSocket` in `WebSocketHandler`

### Router Integration

- Added JWT service initialization in `internal/api/router/router.go`
- Integrated with existing `auth.NewJWTService` and `repository.RefreshTokenRepository`
- All handlers properly wired with JWT service dependency

## Acceptance Criteria

✓ WebSocket endpoint accepts JWT token via query parameter  
✓ Invalid or expired tokens return 401 without establishing connection  
✓ Valid tokens allow WebSocket connection upgrade  
✓ Token validation reuses existing JWT service  
✓ User ID included in client identification  

## Key Decisions

- Token passed via query parameter (not header) for WebSocket compatibility
- Reused existing JWT service from auth package to avoid duplication
- User identification format: `user_{userID}_{ip}_{timestamp}`

## Threat Model Mitigations

| Threat | Disposition | Mitigation |
|--------|-------------|------------|
| T-06-01: Token in URL | accept | HTTPS in production; short-lived tokens (30min) |
| T-06-02: Token validation bypass | mitigate | Reuse existing JWT service; no custom validation |
| T-06-03: Invalid token flood | mitigate | Return 401 immediately; rate limiting at HTTP layer |
