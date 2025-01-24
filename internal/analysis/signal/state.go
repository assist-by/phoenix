package signal

// getSymbolState는 심볼별 상태를 가져옵니다
func (d *Detector) getSymbolState(symbol string) *SymbolState {
	d.mu.RLock()
	state, exists := d.states[symbol]
	d.mu.RUnlock()

	if !exists {
		d.mu.Lock()
		state = &SymbolState{}
		d.states[symbol] = state
		d.mu.Unlock()
	}

	return state
}
