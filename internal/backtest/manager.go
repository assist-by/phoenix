package backtest

import (
	"fmt"
	"log"
	"math"
	"time"

	"github.com/assist-by/phoenix/internal/config"
	"github.com/assist-by/phoenix/internal/domain"
	p "github.com/assist-by/phoenix/internal/position"
)

// Manager는 백테스트용 포지션 관리자입니다
type Manager struct {
	Account     *Account                      // 계정 정보
	Leverage    int                           // 레버리지
	SlippagePct float64                       // 슬리피지 비율 (%)
	TakerFee    float64                       // Taker 수수료 (%)
	MakerFee    float64                       // Maker 수수료 (%)
	Rules       TradingRules                  // 트레이딩 규칙
	SymbolInfos map[string]*domain.SymbolInfo // 심볼 정보
}

// NewManager는 새로운 백테스트 매니저를 생성합니다
func NewManager(cfg *config.Config, symbolInfos map[string]*domain.SymbolInfo) *Manager {
	return &Manager{
		Account: &Account{
			InitialBalance: cfg.Backtest.InitialBalance,
			Balance:        cfg.Backtest.InitialBalance,
			Positions:      make([]*Position, 0),
			ClosedTrades:   make([]*Position, 0),
			HighWaterMark:  cfg.Backtest.InitialBalance,
		},
		Leverage:    cfg.Backtest.Leverage,
		SlippagePct: cfg.Backtest.SlippagePct,
		TakerFee:    0.0004, // 0.04% 기본값
		MakerFee:    0.0002, // 0.02% 기본값
		Rules: TradingRules{
			MaxPositions:    1,    // 기본값: 심볼당 1개 포지션
			MaxRiskPerTrade: 2,    // 기본값: 거래당 2% 리스크
			SlPriority:      true, // 기본값: SL 우선
		},
		SymbolInfos: symbolInfos,
	}
}

// OpenPosition은 새 포지션을 생성합니다
func (m *Manager) OpenPosition(signal domain.SignalInterface, candle domain.Candle) (*Position, error) {
	symbol := signal.GetSymbol()
	signalType := signal.GetType()

	// 이미 열린 포지션이 있는지 확인
	for _, pos := range m.Account.Positions {
		if pos.Symbol == symbol {
			return nil, fmt.Errorf("이미 %s 심볼에 열린 포지션이 있습니다", symbol)
		}
	}

	// 포지션 사이드 결정
	side := p.GetPositionSideFromSignal(signalType)

	// 진입가 결정 (슬리피지 적용)
	entryPrice := signal.GetPrice()
	orderSide := p.GetOrderSideForEntry(side)
	if orderSide == domain.Buy { // Long 진입 = Buy
		entryPrice *= (1 + m.SlippagePct/100)
	} else { // Short 진입 = Sell
		entryPrice *= (1 - m.SlippagePct/100)
	}

	var stepSize, tickSize float64
	var pricePrecision int
	if info, exists := m.SymbolInfos[symbol]; exists {
		stepSize = info.StepSize
		tickSize = info.TickSize
		pricePrecision = info.PricePrecision
	} else {
		// 기본값 설정
		stepSize = 0.001
		tickSize = 0.01
		pricePrecision = 2
	}

	// 포지션 크기 계산
	sizingConfig := p.SizingConfig{
		AccountBalance:   m.Account.Balance,
		AvailableBalance: m.Account.Balance, // 백테스트에서는 동일한 값 사용
		Leverage:         m.Leverage,
		MaxAllocation:    m.Rules.MaxRiskPerTrade / 100, // %를 소수점으로 변환
		StepSize:         stepSize,                      // 심볼 정보에서 가져오는 값
		TickSize:         tickSize,                      // 심볼 정보에서 가져오는 값
		MinNotional:      0,                             // 백테스트에서는 최소값 제약 없음
		MaintMarginRate:  0.01,                          // 기본값 적용 또는 백테스트 설정에서 가져오기
	}

	posResult, err := p.CalculatePositionSize(entryPrice, sizingConfig)
	if err != nil {
		return nil, fmt.Errorf("포지션 크기 계산 실패: %w", err)
	}
	quantity := posResult.Quantity

	// 심볼 정보가 있다면 최소 단위에 맞게 조정
	if info, exists := m.SymbolInfos[symbol]; exists {
		quantity = domain.AdjustQuantity(quantity, info.StepSize, info.QuantityPrecision)
	}

	// 포지션 생성에 필요한 자금이 충분한지 확인
	requiredMargin := (posResult.PositionValue / float64(m.Leverage))
	entryFee := posResult.PositionValue * m.TakerFee

	if m.Account.Balance < (requiredMargin + entryFee) {
		return nil, fmt.Errorf("잔고 부족: 필요 %.2f, 보유 %.2f", requiredMargin+entryFee, m.Account.Balance)
	}

	// 수수료 차감
	m.Account.Balance -= entryFee

	// TP/SL 가격 조정
	adjustedStopLoss := domain.AdjustPrice(signal.GetStopLoss(), tickSize, pricePrecision)
	adjustedTakeProfit := domain.AdjustPrice(signal.GetTakeProfit(), tickSize, pricePrecision)

	// 새 포지션 생성
	position := &Position{
		Symbol:     symbol,
		Side:       side,
		EntryPrice: entryPrice,
		Quantity:   math.Abs(quantity),
		EntryTime:  candle.OpenTime,
		StopLoss:   adjustedStopLoss,
		TakeProfit: adjustedTakeProfit,
		Status:     Open,
		ExitReason: NoExit,
	}

	// 포지션 목록에 추가
	m.Account.Positions = append(m.Account.Positions, position)

	return position, nil
}

// ClosePosition은 특정 포지션을 청산합니다
func (m *Manager) ClosePosition(position *Position, closePrice float64, closeTime time.Time, reason ExitReason) error {
	if position.Status == Closed {
		return fmt.Errorf("이미 청산된 포지션입니다")
	}

	// 청산가가 유효한지 확인 (0이나 음수인 경우 처리)
	if closePrice <= 0 {
		log.Printf("경고: 유효하지 않은 청산가 (%.2f), 현재 가격으로 대체합니다", closePrice)
		return fmt.Errorf("유효하지 않은 청산가: %.2f", closePrice)
	}

	// 청산 시간이 진입 시간보다 이전이면 경고 로그
	if closeTime.Before(position.EntryTime) {
		log.Printf("경고: 청산 시간이 진입 시간보다 이전입니다 (포지션: %s, 진입: %s, 청산: %s)",
			position.Symbol, position.EntryTime.Format("2006-01-02 15:04:05"),
			closeTime.Format("2006-01-02 15:04:05"))
		// 청산 시간을 진입 시간 이후로 조정
		closeTime = position.EntryTime.Add(time.Minute)
	}

	// 특히 Signal Reversal 경우 로그 추가
	if reason == SignalReversal {
		log.Printf("Signal Reversal 청산: %s %s, 진입가: %.2f, 청산가: %.2f (시간: %s)",
			position.Symbol, string(position.Side), position.EntryPrice, closePrice,
			time.Now().Format("2006-01-02 15:04:05"))
	}

	// 청산가 조정 (슬리피지 적용)
	exitSide := p.GetOrderSideForExit(position.Side)
	if exitSide == domain.Sell { // Long 청산 = Sell
		closePrice *= (1 - m.SlippagePct/100)
	} else { // Short 청산 = Buy
		closePrice *= (1 + m.SlippagePct/100)
	}

	// 포지션 가치 계산
	positionValue := position.Quantity * closePrice

	// 수수료 계산
	closeFee := positionValue * m.TakerFee

	log.Printf("DEBUG: 청산 직전 포지션 정보 - 심볼: %s, 사이드: %s, 수량: %.8f, 진입가: %.2f, 청산가: %.2f",
		position.Symbol, position.Side, position.Quantity, position.EntryPrice, closePrice)

	// PnL 계산
	var pnl float64
	if position.Side == domain.LongPosition {
		pnl = (closePrice - position.EntryPrice) * position.Quantity
	} else {
		pnl = (position.EntryPrice - closePrice) * position.Quantity
	}

	log.Printf("DEBUG: PnL 계산 결과 - PnL: %.2f, 사이드: %s, 진입가: %.2f, 청산가: %.2f, 수량: %.8f",
		pnl, position.Side, position.EntryPrice, closePrice, position.Quantity)

	// 수수료 차감
	pnl -= closeFee

	// 레버리지 적용 (선물)
	pnl *= float64(m.Leverage)

	// 포지션 초기 가치 계산 (레버리지 적용 전)
	initialValue := position.EntryPrice * position.Quantity

	log.Printf("DEBUG: 초기 포지션 가치 - 가치: %.2f, 진입가: %.2f, 수량: %.8f",
		initialValue, position.EntryPrice, position.Quantity)

	// PnL 퍼센트 계산 전에 초기 가치가 0인지 체크 (0으로 나누기 방지)
	var pnlPercentage float64
	if initialValue > 0 {
		pnlPercentage = (pnl / initialValue) * 100
	} else {
		pnlPercentage = 0 // 기본값 설정
		log.Printf("경고: 포지션 초기 가치가 0 또는 음수입니다 (심볼: %s)", position.Symbol)
	}

	// 포지션 상태 업데이트
	position.ClosePrice = closePrice
	position.CloseTime = closeTime
	position.Status = Closed
	position.ExitReason = reason
	position.PnL = pnl
	position.PnLPercentage = pnlPercentage

	// 계정 잔고 업데이트
	m.Account.Balance += pnl

	// 계정 기록 업데이트
	m.Account.ClosedTrades = append(m.Account.ClosedTrades, position)

	// 포지션 목록에서 제거
	for i, p := range m.Account.Positions {
		if p == position {
			m.Account.Positions = append(m.Account.Positions[:i], m.Account.Positions[i+1:]...)
			break
		}
	}

	return nil
}

// UpdatePositions은 새 캔들 데이터로 모든 포지션을 업데이트합니다
func (m *Manager) UpdatePositions(currentCandle domain.Candle, signal domain.SignalInterface) []*Position {
	symbol := currentCandle.Symbol
	closedPositions := make([]*Position, 0)

	// 현재 열린 포지션 중 해당 심볼에 대한 포지션 확인
	for _, position := range m.Account.Positions {
		if position.Symbol != symbol {
			continue
		}

		// 1. TP/SL 도달 여부 확인
		tpHit := false
		slHit := false

		if position.Side == domain.LongPosition {
			// 롱 포지션: 고가가 TP 이상이면 TP 도달, 저가가 SL 이하면 SL 도달
			if currentCandle.High >= position.TakeProfit {
				tpHit = true
			}
			if currentCandle.Low <= position.StopLoss {
				slHit = true
			}
		} else {
			// 숏 포지션: 저가가 TP 이하면 TP 도달, 고가가 SL 이상이면 SL 도달
			if currentCandle.Low <= position.TakeProfit {
				tpHit = true
			}
			if currentCandle.High >= position.StopLoss {
				slHit = true
			}
		}

		// 2. 시그널 반전 여부 확인
		signalReversal := false
		var reversalSignalType domain.SignalType = domain.NoSignal

		if signal != nil && signal.GetType() != domain.NoSignal {
			currentPositionSide := position.Side
			newPositionSide := p.GetPositionSideFromSignal(signal.GetType())

			if currentPositionSide != domain.BothPosition && // 헤지모드가 아닌 경우
				newPositionSide != domain.BothPosition && // 유효한 시그널인 경우
				currentPositionSide != newPositionSide { // 방향이 다른 경우
				signalReversal = true
				reversalSignalType = signal.GetType()
			}
		}

		// 3. 청산 처리
		// 동일 캔들에서 TP와 SL 모두 도달하면 SL 우선 처리 (Rules.SlPriority 설정에 따라)
		if slHit && (m.Rules.SlPriority || !tpHit) {
			// SL 청산 (저점 또는 고점이 아닌 SL 가격으로 청산)
			m.ClosePosition(position, position.StopLoss, currentCandle.CloseTime, StopLossHit)
			closedPositions = append(closedPositions, position)
		} else if tpHit {
			// TP 청산 (TP 가격으로 청산)
			m.ClosePosition(position, position.TakeProfit, currentCandle.CloseTime, TakeProfitHit)
			closedPositions = append(closedPositions, position)
		} else if signalReversal {
			// 시그널 반전으로 청산 (현재 캔들 종가로 청산)
			if err := m.ClosePosition(position, currentCandle.Close, currentCandle.CloseTime, SignalReversal); err != nil {
				// 오류 처리 (예: 청산가가 유효하지 않은 경우)
				log.Printf("시그널 반전으로 청산 실패 (%s): %v", symbol, err)
				continue
			}
			closedPositions = append(closedPositions, position)

			// 시그널 반전 후 즉시 신규 포지션 진입
			if reversalSignalType != domain.NoSignal && signal != nil {
				// 새로운 포지션 생성
				_, err := m.OpenPosition(signal, currentCandle)
				if err != nil {
					log.Printf("시그널 반전 후 새 포지션 진입 실패 (%s): %v", symbol, err)
				} else {
					log.Printf("시그널 반전 후 즉시 %s %s 포지션 진입 @ %.2f",
						symbol, signal.GetType().String(), signal.GetPrice())
				}
			}
		}
	}

	return closedPositions
}

// UpdateEquity는 계정 자산을 업데이트합니다
func (m *Manager) UpdateEquity(candles map[string]domain.Candle) {
	// 총 자산 계산 (잔고 + 열린 포지션의 현재 가치)
	equity := m.Account.Balance

	// 모든 열린 포지션에 대해 미실현 손익 계산
	for _, position := range m.Account.Positions {
		// 해당 심볼의 최신 캔들 가져오기
		candle, exists := candles[position.Symbol]
		if !exists {
			continue
		}

		// 포지션 현재 가치 계산
		var unrealizedPnl float64
		if position.Side == domain.LongPosition {
			unrealizedPnl = (candle.Close - position.EntryPrice) * position.Quantity
		} else {
			unrealizedPnl = (position.EntryPrice - candle.Close) * position.Quantity
		}

		// 레버리지 적용
		unrealizedPnl *= float64(m.Leverage)

		// 총 자산에 추가
		equity += unrealizedPnl
	}

	// 계정 자산 업데이트
	m.Account.Equity = equity

	// 최고 자산 갱신
	if equity > m.Account.HighWaterMark {
		m.Account.HighWaterMark = equity
	}

	// 현재 낙폭 계산
	if m.Account.HighWaterMark > 0 {
		currentDrawdown := (m.Account.HighWaterMark - equity) / m.Account.HighWaterMark * 100
		m.Account.Drawdown = currentDrawdown

		// 최대 낙폭 갱신
		if currentDrawdown > m.Account.MaxDrawdown {
			m.Account.MaxDrawdown = currentDrawdown
		}
	}
}

// GetBacktestResult는 백테스트 결과를 계산합니다
func (m *Manager) GetBacktestResult(startTime, endTime time.Time, symbol string, interval domain.TimeInterval) *Result {
	totalTrades := len(m.Account.ClosedTrades)
	winningTrades := 0
	losingTrades := 0
	totalProfitPct := 0.0
	validTradeCount := 0

	// 포지션 분석
	for _, trade := range m.Account.ClosedTrades {
		// NaN 값 검사 추가
		if !math.IsNaN(trade.PnLPercentage) {
			if trade.PnL > 0 {
				winningTrades++
			} else {
				losingTrades++
			}
			totalProfitPct += trade.PnLPercentage
			validTradeCount++
		} else {
			// NaN인 경우 로그에 기록 (디버깅용)
			log.Printf("경고: NaN 수익률이 발견됨 (심볼: %s, 포지션: %s, 청산이유: %d)",
				trade.Symbol, string(trade.Side), trade.ExitReason)
		}
	}

	// 승률 계산
	var winRate float64
	if totalTrades > 0 {
		winRate = float64(winningTrades) / float64(totalTrades) * 100
	}

	// 평균 수익률 계산
	var avgReturn float64
	if totalTrades > 0 {
		avgReturn = totalProfitPct / float64(totalTrades)
	}

	// 누적 수익률 계산
	cumulativeReturn := 0.0
	if m.Account.InitialBalance > 0 {
		cumulativeReturn = (m.Account.Balance - m.Account.InitialBalance) / m.Account.InitialBalance * 100
	}

	// 결과 생성
	return &Result{
		TotalTrades:      validTradeCount,
		WinningTrades:    winningTrades,
		LosingTrades:     losingTrades,
		WinRate:          winRate,
		CumulativeReturn: cumulativeReturn,
		AverageReturn:    avgReturn,
		MaxDrawdown:      m.Account.MaxDrawdown,
		Trades:           m.convertTradesToResultTrades(),
		StartTime:        startTime,
		EndTime:          endTime,
		Symbol:           symbol,
		Interval:         interval,
	}
}

// convertTradesToResultTrades는 내부 포지션 기록을 결과용 Trade 구조체로 변환합니다
func (m *Manager) convertTradesToResultTrades() []Trade {
	trades := make([]Trade, len(m.Account.ClosedTrades))

	for i, position := range m.Account.ClosedTrades {
		exitReason := ""
		switch position.ExitReason {
		case StopLossHit:
			exitReason = "SL"
		case TakeProfitHit:
			exitReason = "TP"
		case SignalReversal:
			exitReason = "Signal Reversal"
		case EndOfBacktest:
			exitReason = "End of Backtest"
		}

		trades[i] = Trade{
			EntryTime:  position.EntryTime,
			ExitTime:   position.CloseTime,
			EntryPrice: position.EntryPrice,
			ExitPrice:  position.ClosePrice,
			Side:       position.Side,
			ProfitPct:  position.PnLPercentage,
			ExitReason: exitReason,
		}
	}

	return trades
}
