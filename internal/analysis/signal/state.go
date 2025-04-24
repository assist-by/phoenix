package signal

// getSymbolState는 심볼별 상태를 가져옵니다
func (d *Detector) getSymbolState(symbol string) *SymbolState {
	d.mu.RLock()
	state, exists := d.states[symbol]
	d.mu.RUnlock()

	if !exists {
		d.mu.Lock()
		state = &SymbolState{
			PendingSignal:  NoSignal,
			WaitedCandles:  0,
			MaxWaitCandles: d.maxWaitCandles,
		}
		d.states[symbol] = state
		d.mu.Unlock()
	}

	return state
}

// resetPendingState는 심볼의 대기 상태를 초기화합니다
func (d *Detector) resetPendingState(state *SymbolState) {
	state.PendingSignal = NoSignal
	state.WaitedCandles = 0
}
