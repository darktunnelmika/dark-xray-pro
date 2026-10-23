// SPDX-License-Identifier: MPL-2.0
// Lab-only overlay for the exact upstream Xray revision in apply_vless_sessions.py.
package inbound

import (
 "context"
 "net"
 "time"

 "github.com/xtls/xray-core/common/errors"
 "github.com/xtls/xray-core/common/protocol"
 "github.com/xtls/xray-core/proxy/vless"
)

type proUserSession struct { cancel context.CancelFunc }

// Serialize user mutation with post-authentication registration. MemoryUser
// pointer identity is the generation token: same email/UUID is not a new grant
// for a handshake which authenticated before removal.
func (h *Handler) trackUserSession(parent context.Context, user *protocol.MemoryUser, conn net.Conn) (context.Context, func(), error) {
 h.proUserMu.Lock()
 defer h.proUserMu.Unlock()
 if user == nil || conn == nil || h.proUserClosed || parent.Err() != nil { return nil, nil, errors.New("user session is not authorized") }
 account, ok := user.Account.(*vless.MemoryAccount)
 if !ok || account.ID == nil || h.validator.Get(account.ID.UUID()) != user || (user.Email != "" && h.validator.GetByEmail(user.Email) != user) {
  return nil, nil, errors.New("user changed during authentication")
 }
 ctx, cancel := context.WithCancel(parent)
 session := &proUserSession{cancel: cancel}
 if h.proUserSessions == nil { h.proUserSessions = make(map[*protocol.MemoryUser]map[*proUserSession]struct{}) }
 if h.proUserSessions[user] == nil { h.proUserSessions[user] = make(map[*proUserSession]struct{}) }
 h.proUserSessions[user][session] = struct{}{}
 // No permanent goroutine per stream. Cancel reaches dispatch/outbound tasks;
 // Close affects only this authenticated logical stream, not its source IP.
 stopClose := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()); _ = conn.Close() })
 release := func() {
  h.proUserMu.Lock()
  delete(h.proUserSessions[user], session)
  if len(h.proUserSessions[user]) == 0 { delete(h.proUserSessions, user) }
  h.proUserMu.Unlock()
  stopClose()
  cancel()
 }
 return ctx, release, nil
}

func (h *Handler) removeTrackedUser(email string) error {
 h.proUserMu.Lock()
 if h.proUserClosed { h.proUserMu.Unlock(); return errors.New("inbound is closed") }
 user := h.validator.GetByEmail(email)
 if err := h.validator.Del(email); err != nil { h.proUserMu.Unlock(); return err }
 sessions := h.proUserSessions[user]
 delete(h.proUserSessions, user)
 h.proUserMu.Unlock()
 // Never hold an admission lock while invoking cancellation/stream Close.
 for session := range sessions { session.cancel() }
 h.RemoveReverse(user)
 return nil
}

func (h *Handler) stopUserSessions() {
 h.proUserMu.Lock()
 h.proUserClosed = true
 groups := h.proUserSessions
 h.proUserSessions = nil
 h.proUserMu.Unlock()
 for _, sessions := range groups { for session := range sessions { session.cancel() } }
}
