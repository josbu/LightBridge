package service

// SetOnUpdateCallback replaces callbacks invoked when settings are updated.
// This is kept for compatibility with older callers; new code should prefer
// AddOnUpdateCallback so independent subsystems do not overwrite each other.
func (s *SettingService) SetOnUpdateCallback(callback func()) {
	if s == nil {
		return
	}
	s.onUpdateMu.Lock()
	defer s.onUpdateMu.Unlock()
	s.onUpdateCallbacks = nil
	if callback != nil {
		s.onUpdateCallbacks = append(s.onUpdateCallbacks, callback)
	}
}

// AddOnUpdateCallback registers an additional settings update callback.
func (s *SettingService) AddOnUpdateCallback(callback func()) {
	if s == nil || callback == nil {
		return
	}
	s.onUpdateMu.Lock()
	defer s.onUpdateMu.Unlock()
	s.onUpdateCallbacks = append(s.onUpdateCallbacks, callback)
}

func (s *SettingService) runOnUpdateCallbacks() {
	if s == nil {
		return
	}
	s.InvalidateProgressiveFeatureSnapshot()
	s.onUpdateMu.RLock()
	callbacks := append([]func(){}, s.onUpdateCallbacks...)
	s.onUpdateMu.RUnlock()
	for _, callback := range callbacks {
		callback()
	}
}

// SetVersion sets the application version for injection into public settings
func (s *SettingService) SetVersion(version string) {
	s.version = version
}
