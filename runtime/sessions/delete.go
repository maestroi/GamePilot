package sessions

import (
	"errors"
	"fmt"
)

// ErrSessionNotTerminal is returned when retained session history is removed
// before the owned runner has reached a terminal state. Active sessions must be
// stopped through Stop and allowed to finish before they can be deleted.
var ErrSessionNotTerminal = errors.New("sessions: session is not terminal")

// Delete removes one terminal session and its retained frame/replay data from
// the manager. It never cancels or otherwise manipulates a running emulator.
func (m *Manager) Delete(id string) error {
	if m == nil {
		return fmt.Errorf("%w: %s", ErrSessionNotFound, id)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	s := m.sessions[id]
	if s == nil {
		return fmt.Errorf("%w: %s", ErrSessionNotFound, id)
	}

	s.mu.RLock()
	isTerminal := terminal(s.snap.Status)
	s.mu.RUnlock()
	if !isTerminal {
		return fmt.Errorf("%w: %s", ErrSessionNotTerminal, id)
	}

	delete(m.sessions, id)
	return nil
}
