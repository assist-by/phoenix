# phoenix
## Project Structure

```
phoenix/
├── backtest/
    ├── data_fetcher.py
    ├── indicators.py
    ├── main.py
    └── visualizer.py
├── cmd/
    └── trader/
    │   └── main.go
└── internal/
    ├── analysis/
        ├── indicator/
        │   ├── ema.go
        │   ├── indicator_test.go
        │   ├── macd.go
        │   ├── sar.go
        │   └── types.go
        └── signal/
        │   ├── detector.go
        │   ├── signal_test.go
        │   ├── state.go
        │   └── types.go
    ├── config/
        └── config.go
    ├── market/
        ├── client.go
        ├── collector.go
        └── types.go
    ├── notification/
        ├── discord/
        │   ├── client.go
        │   ├── embed.go
        │   ├── signal.go
        │   └── webhook.go
        └── types.go
    └── scheduler/
        └── scheduler.go
```

## backtest/data_fetcher.py
```py
import ccxt
import pandas as pd
from datetime import datetime
import logging

# 로깅 설정
logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(name)s - %(levelname)s - %(message)s')
logger = logging.getLogger('DataFetcher')

class BinanceDataFetcher:
    """바이낸스에서 OHLCV 데이터를 가져오는 클래스"""
    
    def __init__(self, api_key=None, api_secret=None):
        """
        바이낸스 데이터 페처 초기화
        
        Args:
            api_key (str, optional): 바이낸스 API 키. 기본값은 None.
            api_secret (str, optional): 바이낸스 API 시크릿. 기본값은 None.
        """
        self.exchange = ccxt.binance({
            'apiKey': api_key,
            'secret': api_secret,
            'enableRateLimit': True,  # API 호출 제한 준수
        })
        logger.info("바이낸스 데이터 페처가 초기화되었습니다.")
    
    def fetch_ohlcv(self, symbol='BTC/USDT', timeframe='4h', limit=500):
        """
        바이낸스에서 OHLCV 데이터를 가져옴
        
        Args:
            symbol (str): 심볼 (예: 'BTC/USDT'). 기본값은 'BTC/USDT'.
            timeframe (str): 시간 프레임 (예: '1m', '5m', '1h', '4h', '1d'). 기본값은 '4h'.
            limit (int): 가져올 캔들 개수. 기본값은 500.
            
        Returns:
            pandas.DataFrame: OHLCV 데이터를 담은 DataFrame. 실패 시 None 반환.
        """
        logger.info(f"{symbol} {timeframe} 데이터 다운로드 중 (limit: {limit})...")
        
        try:
            # 바이낸스에서 데이터 가져오기
            ohlcv = self.exchange.fetch_ohlcv(symbol, timeframe, limit=limit)
            
            # 데이터프레임으로 변환
            df = pd.DataFrame(ohlcv, columns=['timestamp', 'open', 'high', 'low', 'close', 'volume'])
            
            # 타임스탬프를 datetime으로 변환
            df['timestamp'] = pd.to_datetime(df['timestamp'], unit='ms')
            df.set_index('timestamp', inplace=True)
            
            logger.info(f"데이터 다운로드 완료: {len(df)} 개의 캔들")
            return df
            
        except Exception as e:
            logger.error(f"데이터 다운로드 중 오류 발생: {str(e)}")
            return None
```
## backtest/indicators.py
```py
import pandas as pd
import numpy as np
from ta.trend import MACD, PSARIndicator, EMAIndicator
import logging

# 로깅 설정
logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(name)s - %(levelname)s - %(message)s')
logger = logging.getLogger('Indicators')

class TechnicalIndicators:
    """기술적 지표 계산 클래스"""
    
    def __init__(self, df=None):
        """
        기술적 지표 클래스 초기화
        
        Args:
            df (pandas.DataFrame, optional): OHLCV 데이터. 기본값은 None.
        """
        self.df = df
        logger.info("기술적 지표 클래스가 초기화되었습니다.")
    
    def set_data(self, df):
        """
        분석할 데이터 설정
        
        Args:
            df (pandas.DataFrame): OHLCV 데이터
        """
        self.df = df
    
    def add_ema(self, period=200, column='close'):
        """
        지수이동평균(EMA) 추가
        
        Args:
            period (int): EMA 기간. 기본값은 200.
            column (str): 계산에 사용할 컬럼명. 기본값은 'close'.
            
        Returns:
            pandas.DataFrame: 지표가 추가된 데이터프레임
        """
        if self.df is None:
            logger.error("데이터가 설정되지 않았습니다.")
            return None
        
        try:
            indicator = EMAIndicator(close=self.df[column], window=period)
            self.df[f'ema{period}'] = indicator.ema_indicator()
            logger.info(f"EMA{period} 지표가 추가되었습니다.")
            return self.df
        except Exception as e:
            logger.error(f"EMA 계산 중 오류 발생: {str(e)}")
            return self.df
    
    def add_macd(self, fast_period=12, slow_period=26, signal_period=9, column='close'):
        """
        MACD(Moving Average Convergence Divergence) 추가
        
        Args:
            fast_period (int): 빠른 EMA 기간. 기본값은 12.
            slow_period (int): 느린 EMA 기간. 기본값은 26.
            signal_period (int): 시그널 기간. 기본값은 9.
            column (str): 계산에 사용할 컬럼명. 기본값은 'close'.
            
        Returns:
            pandas.DataFrame: 지표가 추가된 데이터프레임
        """
        if self.df is None:
            logger.error("데이터가 설정되지 않았습니다.")
            return None
        
        try:
            indicator = MACD(
                close=self.df[column], 
                window_fast=fast_period, 
                window_slow=slow_period, 
                window_sign=signal_period
            )
            self.df['macd'] = indicator.macd()
            self.df['macd_signal'] = indicator.macd_signal()
            self.df['macd_histogram'] = indicator.macd_diff()
            logger.info(f"MACD({fast_period},{slow_period},{signal_period}) 지표가 추가되었습니다.")
            return self.df
        except Exception as e:
            logger.error(f"MACD 계산 중 오류 발생: {str(e)}")
            return self.df
    
    def add_parabolic_sar(self, step=0.02, max_step=0.2):
        """
        Parabolic SAR 추가
        
        Args:
            step (float): 가속도 인자 초기값. 기본값은 0.02.
            max_step (float): 가속도 인자 최대값. 기본값은 0.2.
            
        Returns:
            pandas.DataFrame: 지표가 추가된 데이터프레임
        """
        if self.df is None:
            logger.error("데이터가 설정되지 않았습니다.")
            return None
        
        try:
            indicator = PSARIndicator(
                high=self.df['high'], 
                low=self.df['low'], 
                close=self.df['close'],
                step=step,
                max_step=max_step
            )
            self.df['psar'] = indicator.psar()
            logger.info(f"Parabolic SAR(step={step}, max_step={max_step}) 지표가 추가되었습니다.")
            return self.df
        except Exception as e:
            logger.error(f"Parabolic SAR 계산 중 오류 발생: {str(e)}")
            return self.df
    
    def add_all_indicators(self):
        """
        모든 기본 지표 추가 (EMA200, MACD, Parabolic SAR)
        
        Returns:
            pandas.DataFrame: 모든 지표가 추가된 데이터프레임
        """
        if self.df is None:
            logger.error("데이터가 설정되지 않았습니다.")
            return None
        
        logger.info("모든 기본 지표 추가 중...")
        self.add_ema(period=200)
        self.add_macd()
        self.add_parabolic_sar()
        return self.df
```
## backtest/main.py
```py
import logging
import argparse
from data_fetcher import BinanceDataFetcher
from indicators import TechnicalIndicators
from visualizer import ChartVisualizer

# 로깅 설정
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s',
    handlers=[
        logging.FileHandler("trading_analysis.log"),
        logging.StreamHandler()
    ]
)
logger = logging.getLogger('Main')

def parse_args():
    """명령줄 인자 파싱"""
    parser = argparse.ArgumentParser(description='암호화폐 기술적 분석 도구')
    
    parser.add_argument('--symbol', type=str, default='BTC/USDT',
                        help='분석할 암호화폐 심볼 (기본값: BTC/USDT)')
    
    parser.add_argument('--timeframe', type=str, default='15m',
                        help='분석할 시간대 (기본값: 15m)')
    
    parser.add_argument('--limit', type=int, default=500,
                        help='가져올 캔들 개수 (기본값: 500)')
    
    parser.add_argument('--save', action='store_true',
                        help='차트를 파일로 저장')
    
    parser.add_argument('--output', type=str, default='chart.png',
                        help='저장할 파일 이름 (기본값: chart.png)')
    
    parser.add_argument('--use-mpf', type=bool, default=True,
                        help='mplfinance 라이브러리 사용 여부 (기본값: True)')
    
    return parser.parse_args()

def main():
    """메인 함수"""
    # 명령줄 인자 파싱
    args = parse_args()
    
    logger.info(f"거래 분석 시작: {args.symbol}, {args.timeframe}, {args.limit}개 캔들")
    
    try:
        # 1. 데이터 가져오기
        fetcher = BinanceDataFetcher()
        df = fetcher.fetch_ohlcv(symbol=args.symbol, timeframe=args.timeframe, limit=args.limit)
        
        if df is None or len(df) == 0:
            logger.error("데이터를 가져오지 못했습니다.")
            return
        
        logger.info(f"데이터 다운로드 완료: {len(df)}개 캔들")
        
        # 2. 기술적 지표 계산
        indicators = TechnicalIndicators(df)
        df = indicators.add_all_indicators()
        
        # 3. 차트 시각화
        visualizer = ChartVisualizer(df)
        title = f"{args.symbol} - {args.timeframe} 차트"
        visualizer.plot_chart(use_mplfinance=args.use_mpf, title=title)
        
        # 4. 차트 저장 및 표시
        if args.save:
            visualizer.save_chart(filename=args.output)
        
        # 차트 표시
        visualizer.show_chart()
        
        # 5. 데이터 요약 정보 출력
        print("\n=== 데이터 요약 ===")
        print(f"심볼: {args.symbol}")
        print(f"시간대: {args.timeframe}")
        print(f"기간: {df.index[0]} ~ {df.index[-1]}")
        print(f"캔들 수: {len(df)}")
        print(f"시작가: {df['close'].iloc[0]:.2f}")
        print(f"종료가: {df['close'].iloc[-1]:.2f}")
        print(f"변동률: {((df['close'].iloc[-1] / df['close'].iloc[0]) - 1) * 100:.2f}%")
        
        return df
        
    except Exception as e:
        logger.error(f"실행 중 오류 발생: {str(e)}")
        return None

if __name__ == "__main__":
    df = main()
```
## backtest/visualizer.py
```py
import pandas as pd
import numpy as np
import matplotlib.pyplot as plt
import matplotlib.dates as mpdates
import logging

# 로깅 설정
logging.basicConfig(level=logging.INFO, format='%(asctime)s - %(name)s - %(levelname)s - %(message)s')
logger = logging.getLogger('Visualizer')

class ChartVisualizer:
    """차트 시각화를 담당하는 클래스"""
    
    def __init__(self, df=None):
        """
        차트 시각화 클래스 초기화
        
        Args:
            df (pandas.DataFrame, optional): 시각화할 데이터. 기본값은 None.
        """
        self.df = df
        self.fig = None
        self.axes = None
        logger.info("차트 시각화 클래스가 초기화되었습니다.")
        
        # mplfinance 임포트 시도
        try:
            import mplfinance as mpf
            self.mpf = mpf
            self.has_mpf = True
            logger.info("mplfinance 라이브러리를 사용합니다.")
        except ImportError:
            logger.warning("mplfinance 라이브러리가 설치되지 않았습니다. 기본 matplotlib를 사용합니다.")
            self.has_mpf = False
    
    def set_data(self, df):
        """
        시각화할 데이터 설정
        
        Args:
            df (pandas.DataFrame): 시각화할 데이터
        """
        self.df = df
    
    def _prepare_data(self, max_candles=500):
        """
        시각화를 위한 데이터 준비
        
        Args:
            max_candles (int): 최대 캔들 수. 기본값은 500.
            
        Returns:
            pandas.DataFrame: 시각화용으로 준비된 데이터프레임
        """
        if self.df is None:
            logger.error("데이터가 설정되지 않았습니다.")
            return None
        
        # 필요한 기술적 지표 칼럼이 있는지 확인
        required_columns = ['ema200', 'macd', 'macd_signal', 'macd_histogram', 'psar']
        missing_columns = [col for col in required_columns if col not in self.df.columns]
        
        if missing_columns:
            logger.warning(f"일부 지표 칼럼이 누락되었습니다: {missing_columns}")
        
        # EMA200이 계산된 부분만 사용
        if 'ema200' in self.df.columns:
            df_valid = self.df.dropna(subset=['ema200']).copy()
            logger.info(f"EMA200이 계산된 유효한 데이터: {len(df_valid)}개 캔들")
        else:
            df_valid = self.df.copy()
            logger.warning("EMA200 칼럼이 없습니다. 전체 데이터를 사용합니다.")
        
        # 데이터가 너무 많으면 최근 데이터로 제한
        if len(df_valid) > max_candles:
            df_valid = df_valid.tail(max_candles).copy()
            logger.info(f"데이터를 최근 {max_candles}개 캔들로 제한합니다.")
        
        return df_valid
    
    def plot_basic_chart(self, max_candles=500, title="가격 차트"):
        """
        기본 matplotlib을 사용한 차트 그리기
        
        Args:
            max_candles (int): 최대 캔들 수. 기본값은 500.
            title (str): 차트 제목. 기본값은 "가격 차트".
            
        Returns:
            matplotlib.figure.Figure: 생성된 차트 figure 객체
        """
        df_plot = self._prepare_data(max_candles)
        if df_plot is None:
            return None
        
        # 차트 생성
        self.fig, self.axes = plt.subplots(2, 1, figsize=(15, 10), 
                                          gridspec_kw={'height_ratios': [3, 1]}, 
                                          sharex=True)
        
        # 메인 차트 (캔들 + 지표)
        ax1 = self.axes[0]
        
        # 캔들스틱 그리기
        for i in range(len(df_plot)):
            # 양봉/음봉 구분
            color = 'green' if df_plot['close'].iloc[i] >= df_plot['open'].iloc[i] else 'red'
            # 캔들 바디
            ax1.plot([i, i], [df_plot['open'].iloc[i], df_plot['close'].iloc[i]], 
                    color=color, linewidth=4)
            # 위아래 꼬리
            ax1.plot([i, i], [df_plot['low'].iloc[i], df_plot['high'].iloc[i]], 
                    color=color, linewidth=1)
        
        # 200 EMA
        if 'ema200' in df_plot.columns:
            ax1.plot(range(len(df_plot)), df_plot['ema200'], color='blue', linewidth=1.5, label='EMA 200')
        
        # Parabolic SAR
        if 'psar' in df_plot.columns:
            ax1.scatter(range(len(df_plot)), df_plot['psar'], color='purple', s=20, label='Parabolic SAR')
        
        ax1.set_title(title, fontsize=15)
        ax1.set_ylabel('가격')
        ax1.legend(loc='upper left')
        ax1.grid(alpha=0.3)
        
        # MACD 차트
        ax2 = self.axes[1]
        
        if all(col in df_plot.columns for col in ['macd', 'macd_signal', 'macd_histogram']):
            ax2.plot(range(len(df_plot)), df_plot['macd'], color='blue', linewidth=1, label='MACD')
            ax2.plot(range(len(df_plot)), df_plot['macd_signal'], color='red', linewidth=1, label='Signal')
            
            # MACD 히스토그램
            for i in range(len(df_plot)):
                color = 'green' if df_plot['macd_histogram'].iloc[i] >= 0 else 'red'
                ax2.bar(i, df_plot['macd_histogram'].iloc[i], color=color, alpha=0.5)
            
            ax2.axhline(y=0, color='black', linestyle='-', alpha=0.2)
            ax2.set_ylabel('MACD')
            ax2.legend(loc='upper left')
            ax2.grid(alpha=0.3)
        
        # X축 레이블 설정
        if len(df_plot) > 0:
            tick_positions = np.linspace(0, len(df_plot) - 1, min(10, len(df_plot)), dtype=int)
            tick_labels = [df_plot.index[i].strftime('%Y-%m-%d') for i in tick_positions]
            ax2.set_xticks(tick_positions)
            ax2.set_xticklabels(tick_labels, rotation=45)
        
        plt.tight_layout()
        return self.fig
    
    def plot_mplfinance_chart(self, max_candles=500, title="캔들스틱 차트"):
        """
        mplfinance 라이브러리를 사용한 캔들스틱 차트 그리기
        
        Args:
            max_candles (int): 최대 캔들 수. 기본값은 500.
            title (str): 차트 제목. 기본값은 "캔들스틱 차트".
            
        Returns:
            matplotlib.figure.Figure: 생성된 차트 figure 객체 또는 None(라이브러리 없는 경우)
        """
        if not self.has_mpf:
            logger.error("mplfinance 라이브러리가 설치되지 않았습니다.")
            return None
        
        df_plot = self._prepare_data(max_candles)
        if df_plot is None:
            return None
        
        # 추가 플롯 설정
        addplots = []
        
        # EMA200 추가
        if 'ema200' in df_plot.columns:
            addplots.append(self.mpf.make_addplot(df_plot['ema200'], color='blue', width=1.5))
        
        # Parabolic SAR 추가
        if 'psar' in df_plot.columns:
            addplots.append(self.mpf.make_addplot(df_plot['psar'], type='scatter', markersize=50, marker='.', color='purple'))
        
        # MACD 추가
        if all(col in df_plot.columns for col in ['macd', 'macd_signal']):
            addplots.append(self.mpf.make_addplot(df_plot['macd'], panel=1, color='blue', secondary_y=False))
            addplots.append(self.mpf.make_addplot(df_plot['macd_signal'], panel=1, color='red', secondary_y=False))
        
        # 차트 스타일 설정
        mc = self.mpf.make_marketcolors(up='green', down='red', edge='black', wick='black')
        s = self.mpf.make_mpf_style(marketcolors=mc, gridstyle='-', y_on_right=False)
        
        # 차트 그리기
        try:
            self.fig, self.axes = self.mpf.plot(df_plot, type='candle', style=s, addplot=addplots,
                                              figsize=(15, 10), panel_ratios=(2, 1), returnfig=True,
                                              title=title)
            
            # MACD 히스토그램 수동 추가 (색상 구분)
            if all(col in df_plot.columns for col in ['macd', 'macd_signal', 'macd_histogram']) and len(self.axes) > 2:
                ax_macd = self.axes[2]
                
                # 히스토그램 제로라인
                ax_macd.axhline(y=0, color='black', linestyle='-', alpha=0.2)
                
                # 범례 추가
                self.axes[0].legend(['EMA(200)', 'Price', 'Parabolic SAR'], loc='upper left')
                ax_macd.legend(['MACD', 'Signal Line'], loc='upper left')
            
            return self.fig
            
        except Exception as e:
            logger.error(f"mplfinance 차트 생성 중 오류 발생: {str(e)}")
            return None
    
    def plot_chart(self, use_mplfinance=True, max_candles=500, title=None):
        """
        사용 가능한 라이브러리를 이용해 차트 그리기
        
        Args:
            use_mplfinance (bool): mplfinance 사용 여부. 기본값은 True.
            max_candles (int): 최대 캔들 수. 기본값은 500.
            title (str, optional): 차트 제목. 기본값은 None.
            
        Returns:
            matplotlib.figure.Figure: 생성된 차트 figure 객체
        """
        # 제목 설정
        if title is None:
            symbol = "Unknown"
            for col in ['symbol', 'pair']:
                if hasattr(self.df, col) and getattr(self.df, col) is not None:
                    symbol = getattr(self.df, col)
                    break
            title = f"{symbol} - 기술적 분석 차트"
        
        # mplfinance 라이브러리로 차트 그리기 시도
        if use_mplfinance and self.has_mpf:
            fig = self.plot_mplfinance_chart(max_candles=max_candles, title=title)
            if fig is not None:
                return fig
        
        # 기본 matplotlib로 차트 그리기
        return self.plot_basic_chart(max_candles=max_candles, title=title)
    
    def show_chart(self):
        """차트 표시"""
        if self.fig is not None:
            plt.show()
        else:
            logger.error("표시할 차트가 없습니다. plot_chart() 메서드를 먼저 호출하세요.")
    
    def save_chart(self, filename='chart.png', dpi=300):
        """
        차트를 파일로 저장
        
        Args:
            filename (str): 저장할 파일 이름. 기본값은 'chart.png'.
            dpi (int): 해상도(DPI). 기본값은 300.
        """
        if self.fig is not None:
            self.fig.savefig(filename, dpi=dpi, bbox_inches='tight')
            logger.info(f"차트가 '{filename}'로 저장되었습니다.")
        else:
            logger.error("저장할 차트가 없습니다. plot_chart() 메서드를 먼저 호출하세요.")
```
## cmd/trader/main.go
```go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	osSignal "os/signal"
	"syscall"
	"time"

	"github.com/assist-by/phoenix/internal/analysis/signal"
	"github.com/assist-by/phoenix/internal/config"
	"github.com/assist-by/phoenix/internal/market"
	"github.com/assist-by/phoenix/internal/notification"
	"github.com/assist-by/phoenix/internal/notification/discord"
	"github.com/assist-by/phoenix/internal/scheduler"
)

// CollectorTask는 데이터 수집 작업을 정의합니다
type CollectorTask struct {
	collector *market.Collector
	discord   *discord.Client
}

// Execute는 데이터 수집 작업을 실행합니다
func (t *CollectorTask) Execute(ctx context.Context) error {
	// 데이터 수집 실행
	if err := t.collector.Collect(ctx); err != nil {
		if err := t.discord.SendError(err); err != nil {
			log.Printf("에러 알림 전송 실패: %v", err)
		}
		return err
	}

	return nil
}

func main() {
	// 명령줄 플래그 정의
	buyModeFlag := flag.Bool("buymode", false, "1회 매수 후 종료")
	testLongFlag := flag.Bool("testlong", false, "롱 포지션 테스트 후 종료")
	testShortFlag := flag.Bool("testshort", false, "숏 포지션 테스트 후 종료")

	// 플래그 파싱
	flag.Parse()

	// 컨텍스트 생성
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 로그 설정
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	log.Println("트레이딩 봇 시작...")

	// 설정 로드
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("설정 로드 실패: %v", err)
	}

	// API 키 선택
	apiKey := cfg.Binance.APIKey
	secretKey := cfg.Binance.SecretKey

	// Discord 클라이언트 생성
	discordClient := discord.NewClient(
		cfg.Discord.SignalWebhook,
		cfg.Discord.TradeWebhook,
		cfg.Discord.ErrorWebhook,
		cfg.Discord.InfoWebhook,
		discord.WithTimeout(10*time.Second),
	)

	// 시작 알림 전송
	if err := discordClient.SendInfo("🚀 트레이딩 봇이 시작되었습니다."); err != nil {
		log.Printf("시작 알림 전송 실패: %v", err)
	}

	// 테스트넷 사용 시 테스트넷 API 키로 변경
	if cfg.Binance.UseTestnet {
		apiKey = cfg.Binance.TestAPIKey
		secretKey = cfg.Binance.TestSecretKey

		discordClient.SendInfo("⚠️ 테스트넷 모드로 실행 중입니다. 실제 자산은 사용되지 않습니다.")
	} else {
		discordClient.SendInfo("⚠️ 메인넷 모드로 실행 중입니다. 실제 자산이 사용됩니다!")
	}

	// 바이낸스 클라이언트 생성
	binanceClient := market.NewClient(
		apiKey,
		secretKey,
		market.WithTimeout(10*time.Second),
		market.WithTestnet(cfg.Binance.UseTestnet),
	)
	// 바이낸스 서버와 시간 동기화
	if err := binanceClient.SyncTime(ctx); err != nil {
		log.Printf("바이낸스 서버 시간 동기화 실패: %v", err)
		if err := discordClient.SendError(fmt.Errorf("바이낸스 서버 시간 동기화 실패: %w", err)); err != nil {
			log.Printf("에러 알림 전송 실패: %v", err)
		}
		os.Exit(1)
	}

	// 시그널 감지기 생성
	detector := signal.NewDetector(signal.DetectorConfig{
		EMALength:      200,
		StopLossPct:    0.02,
		TakeProfitPct:  0.04,
		MinHistogram:   0.00005,
		MaxWaitCandles: 3, // 대기 상태 최대 캔들 수 설정
	})

	if *buyModeFlag {
		// Buy Mode 실행
		log.Println("Buy Mode 활성화: 1회 매수 후 종료합니다")

		// 매수 작업 생성
		buyTask := &BuyTask{
			client:   binanceClient,
			discord:  discordClient,
			detector: detector,
			config:   cfg,
		}

		// 매수 실행
		if err := buyTask.Execute(ctx); err != nil {
			log.Printf("매수 실행 중 에러 발생: %v", err)
			if err := discordClient.SendError(err); err != nil {
				log.Printf("에러 알림 전송 실패: %v", err)
			}
			os.Exit(1)
		}

		// 매수 성공 알림 및 종료
		if err := discordClient.SendInfo("✅ 1회 매수 실행 완료. 프로그램을 종료합니다."); err != nil {
			log.Printf("종료 알림 전송 실패: %v", err)
		}

		log.Println("프로그램을 종료합니다.")
		os.Exit(0)
	}

	// 테스트 모드 실행 (플래그 기반)
	if *testLongFlag || *testShortFlag {
		testType := "Long"
		signalType := signal.Long

		if *testShortFlag {
			testType = "Short"
			signalType = signal.Short
		}

		// 테스트할 심볼
		symbol := "BTCUSDT"

		// 현재 가격 정보 가져오기
		candles, err := binanceClient.GetKlines(ctx, symbol, "1m", 1)
		if err != nil {
			log.Fatalf("가격 정보 조회 실패: %v", err)
		}
		currentPrice := candles[0].Close

		// 테스트 시그널 생성
		var testSignal *signal.Signal

		if signalType == signal.Long {
			testSignal = &signal.Signal{
				Type:       signal.Long,
				Symbol:     symbol,
				Price:      currentPrice,
				Timestamp:  time.Now(),
				StopLoss:   currentPrice * 0.99, // 가격의 99% (1% 손절)
				TakeProfit: currentPrice * 1.01, // 가격의 101% (1% 익절)
			}
		} else {
			testSignal = &signal.Signal{
				Type:       signal.Short,
				Symbol:     symbol,
				Price:      currentPrice,
				Timestamp:  time.Now(),
				StopLoss:   currentPrice * 1.01, // 가격의 101% (1% 손절)
				TakeProfit: currentPrice * 0.99, // 가격의 99% (1% 익절)
			}
		}

		// 시그널 알림 전송
		if err := discordClient.SendSignal(testSignal); err != nil {
			log.Printf("시그널 알림 전송 실패: %v", err)
		}

		// 데이터 수집기 생성
		collector := market.NewCollector(
			binanceClient,
			discordClient,
			detector,
			cfg,
			market.WithRetryConfig(market.RetryConfig{
				MaxRetries: 3,
				BaseDelay:  1 * time.Second,
				MaxDelay:   30 * time.Second,
				Factor:     2.0,
			}),
		)

		// executeSignalTrade 직접 호출
		if err := collector.ExecuteSignalTrade(ctx, testSignal); err != nil {
			log.Printf("테스트 매매 실행 중 에러 발생: %v", err)
			if err := discordClient.SendError(err); err != nil {
				log.Printf("에러 알림 전송 실패: %v", err)
			}
			os.Exit(1)
		}

		// 테스트 성공 알림 및 종료
		if err := discordClient.SendInfo(fmt.Sprintf("✅ 테스트 %s 실행 완료. 프로그램을 종료합니다.", testType)); err != nil {
			log.Printf("종료 알림 전송 실패: %v", err)
		}

		log.Println("프로그램을 종료합니다.")
		os.Exit(0)
	}

	// 데이터 수집기 생성
	collector := market.NewCollector(
		binanceClient,
		discordClient,
		detector,
		cfg,
		market.WithRetryConfig(market.RetryConfig{
			MaxRetries: 3,
			BaseDelay:  1 * time.Second,
			MaxDelay:   30 * time.Second,
			Factor:     2.0,
		}),
	)

	// 수집 작업 생성
	task := &CollectorTask{
		collector: collector,
		discord:   discordClient,
	}

	// 스케줄러 생성 (fetchInterval)
	scheduler := scheduler.NewScheduler(cfg.App.FetchInterval, task)

	// 시그널 처리
	sigChan := make(chan os.Signal, 1)
	osSignal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 스케줄러 시작
	go func() {
		if err := scheduler.Start(ctx); err != nil {
			log.Printf("스케줄러 실행 중 에러 발생: %v", err)
			if err := discordClient.SendError(err); err != nil {
				log.Printf("에러 알림 전송 실패: %v", err)
			}
		}
	}()

	// 시그널 대기
	sig := <-sigChan
	log.Printf("시스템 종료 신호 수신: %v", sig)

	// 스케줄러 중지
	scheduler.Stop()

	// 종료 알림 전송
	if err := discordClient.SendInfo("👋 트레이딩 봇이 정상적으로 종료되었습니다."); err != nil {
		log.Printf("종료 알림 전송 실패: %v", err)
	}

	log.Println("프로그램을 종료합니다.")
}

////////////////////////
////////////
////////////
////////////
////////////
////////////
////////////////////////////////////////////////
////////////
////////////
////////////
////////////
////////////
////////////////////////////////////////////////
////////////
////////////
////////////
////////////
////////////
////////////////////////////////////////////////
////////////
////////////
////////////
////////////
////////////
////////////////////////////////////////////////
////////////
////////////
////////////
////////////
////////////
////////////////////////////////////////////////
////////////
////////////
////////////
////////////
////////////
////////////////////////////////////////////////
////////////
////////////
////////////
////////////
////////////
////////////////////////////////////////////////
////////////
////////////
////////////
////////////
////////////
////////////////////////////////////////////////
////////////
////////////
////////////
////////////
////////////
////////////////////////////////////////////////
////////////
////////////
////////////
////////////
////////////
////////////////////////////////////////////////
////////////
////////////
////////////
////////////
////////////
////////////////////////////////////////////////
////////////
////////////
////////////
////////////
////////////
////////////////////////////////////////////////
////////////
////////////
////////////
////////////
////////////
////////////////////////////////////////////////
////////////
////////////
////////////
////////////
////////////
////////////////////////////////////////////////
////////////
////////////
////////////
////////////
////////////
////////////////////////

// BuyTask는 1회 매수 작업을 정의합니다
type BuyTask struct {
	client   *market.Client
	discord  *discord.Client
	detector *signal.Detector
	config   *config.Config
}

// BuyTask의 Execute 함수 시작 부분에 추가
func (t *BuyTask) Execute(ctx context.Context) error {
	// 심볼 설정 (BTCUSDT 고정)
	symbol := "BTCUSDT"

	// 작업 시작 알림
	if err := t.discord.SendInfo(fmt.Sprintf("🚀 %s 1회 매수 시작", symbol)); err != nil {
		log.Printf("작업 시작 알림 전송 실패: %v", err)
	}

	// 1. 기존 포지션 확인
	positions, err := t.client.GetPositions(ctx)
	if err != nil {
		return fmt.Errorf("포지션 조회 실패: %w", err)
	}

	// 기존 포지션이 있는지 확인
	for _, pos := range positions {
		if pos.Symbol == symbol && pos.Quantity != 0 {
			return fmt.Errorf("이미 %s에 대한 포지션이 있습니다. 수량: %.8f, 방향: %s",
				pos.Symbol, pos.Quantity, pos.PositionSide)
		}
	}

	// 2. 열린 주문 확인
	openOrders, err := t.client.GetOpenOrders(ctx, symbol)
	if err != nil {
		return fmt.Errorf("주문 조회 실패: %w", err)
	}

	// 기존 TP/SL 주문이 있는지 확인
	if len(openOrders) > 0 {
		// 기존 주문 취소
		log.Printf("기존 주문 %d개를 취소합니다.", len(openOrders))
		for _, order := range openOrders {
			if err := t.client.CancelOrder(ctx, symbol, order.OrderID); err != nil {
				log.Printf("주문 취소 실패 (ID: %d): %v", order.OrderID, err)
				// 취소 실패해도 계속 진행
			} else {
				log.Printf("주문 취소 성공: %s %s (ID: %d)", order.Type, order.Side, order.OrderID)
			}
		}
	}

	// 매수 실행 로직
	// 1. 잔고 조회
	balances, err := t.client.GetBalance(ctx)
	if err != nil {
		return fmt.Errorf("잔고 조회 실패: %w", err)
	}

	// 2. USDT 잔고 확인
	usdtBalance, exists := balances["USDT"]
	if !exists || usdtBalance.Available <= 0 {
		return fmt.Errorf("USDT 잔고가 부족합니다")
	}

	// 3. 현재 가격 조회 (최근 캔들 사용)
	candles, err := t.client.GetKlines(ctx, symbol, "1m", 1)
	if err != nil {
		return fmt.Errorf("가격 정보 조회 실패: %w", err)
	}

	if len(candles) == 0 {
		return fmt.Errorf("캔들 데이터를 가져오지 못했습니다")
	}

	currentPrice := candles[0].Close

	// 4. 심볼 정보 조회
	symbolInfo, err := t.client.GetSymbolInfo(ctx, symbol)
	if err != nil {
		return fmt.Errorf("심볼 정보 조회 실패: %w", err)
	}

	// 5. HEDGE 모드 설정
	if err := t.client.SetPositionMode(ctx, true); err != nil {
		return fmt.Errorf("HEDGE 모드 설정 실패: %w", err)
	}

	// 6. 레버리지 설정 (5배 고정)
	leverage := 5
	if err := t.client.SetLeverage(ctx, symbol, leverage); err != nil {
		return fmt.Errorf("레버리지 설정 실패: %w", err)
	}

	// 7. 매수 수량 계산 (잔고의 90% 사용)
	// CollectPosition 함수와 동일한 로직 사용
	collector := market.NewCollector(t.client, t.discord, t.detector, t.config)

	// 레버리지 브라켓 정보 조회
	brackets, err := t.client.GetLeverageBrackets(ctx, symbol)
	if err != nil {
		return fmt.Errorf("레버리지 브라켓 조회 실패: %w", err)
	}

	// 해당 심볼의 브라켓 정보 찾기
	var symbolBracket *market.SymbolBrackets
	for _, b := range brackets {
		if b.Symbol == symbol {
			symbolBracket = &b
			break
		}
	}

	if symbolBracket == nil || len(symbolBracket.Brackets) == 0 {
		return fmt.Errorf("레버리지 브라켓 정보가 없습니다")
	}

	// 설정된 레버리지에 맞는 브라켓 찾기
	bracket := findBracket(symbolBracket.Brackets, leverage)
	if bracket == nil {
		return fmt.Errorf("적절한 레버리지 브라켓을 찾을 수 없습니다")
	}

	// 포지션 크기 계산
	positionResult := collector.CalculatePosition(
		usdtBalance.Available,
		usdtBalance.CrossWalletBalance,
		leverage,
		currentPrice,
		symbolInfo.StepSize,
		bracket.MaintMarginRatio,
	)

	// 최소 주문 가치 체크
	if positionResult.PositionValue < symbolInfo.MinNotional {
		return fmt.Errorf("포지션 크기가 최소 주문 가치(%.2f USDT)보다 작습니다", symbolInfo.MinNotional)
	}

	// 8. 주문 수량 정밀도 조정
	adjustedQuantity := market.AdjustQuantity(
		positionResult.Quantity,
		symbolInfo.StepSize,
		symbolInfo.QuantityPrecision,
	)

	// 9. 매수 주문 생성 (LONG 포지션)
	entryOrder := market.OrderRequest{
		Symbol:       symbol,
		Side:         market.Buy,
		PositionSide: market.Long,
		Type:         market.Market,
		Quantity:     adjustedQuantity,
	}

	// 10. 매수 주문 실행
	orderResponse, err := t.client.PlaceOrder(ctx, entryOrder)
	if err != nil {
		return fmt.Errorf("주문 실행 실패: %w", err)
	}

	// 11. 성공 메시지 출력 및 로깅
	log.Printf("매수 주문 성공: %s, 수량: %.8f, 주문 ID: %d",
		symbol, adjustedQuantity, orderResponse.OrderID)

	// 12. 포지션 확인 및 TP/SL 설정
	// 포지션이 실제로 생성되었는지 확인
	maxRetries := 5
	retryInterval := 1 * time.Second
	var position *market.PositionInfo

	for i := 0; i < maxRetries; i++ {
		positions, err := t.client.GetPositions(ctx)
		if err != nil {
			log.Printf("포지션 조회 실패 (시도 %d/%d): %v", i+1, maxRetries, err)
			time.Sleep(retryInterval)
			continue
		}

		for _, pos := range positions {
			if pos.Symbol == symbol && pos.PositionSide == "LONG" && pos.Quantity > 0 {
				position = &pos
				log.Printf("포지션 확인: %s LONG, 수량: %.8f, 진입가: %.2f",
					pos.Symbol, pos.Quantity, pos.EntryPrice)
				break
			}
		}

		if position != nil {
			break
		}

		log.Printf("아직 포지션이 생성되지 않음 (시도 %d/%d), 대기 중...", i+1, maxRetries)
		time.Sleep(retryInterval)
		retryInterval *= 2 // 지수 백오프
	}

	if position == nil {
		return fmt.Errorf("최대 재시도 횟수 초과: 포지션을 찾을 수 없음")
	}

	// 13. TP/SL 설정 (1% 고정)
	actualEntryPrice := position.EntryPrice
	actualQuantity := position.Quantity

	// 원래 계산
	rawStopLoss := actualEntryPrice * 0.999   // 진입가 -1%
	rawTakeProfit := actualEntryPrice * 1.001 // 진입가 +1%

	// 가격 정밀도에 맞게 조정
	// symbolInfo.TickSize와 symbolInfo.PricePrecision 사용
	stopLoss := AdjustPrice(rawStopLoss, symbolInfo.TickSize, symbolInfo.PricePrecision)
	takeProfit := AdjustPrice(rawTakeProfit, symbolInfo.TickSize, symbolInfo.PricePrecision)

	// TP/SL 설정 알림
	if err := t.discord.SendInfo(fmt.Sprintf(
		"TP/SL 설정 중: %s\n진입가: %.2f\n수량: %.8f\n손절가: %.2f (-1%%)\n목표가: %.2f (+1%%)",
		symbol, actualEntryPrice, actualQuantity, stopLoss, takeProfit)); err != nil {
		log.Printf("TP/SL 설정 알림 전송 실패: %v", err)
	}

	// 14. TP/SL 주문 생성
	slOrder := market.OrderRequest{
		Symbol:       symbol,
		Side:         market.Sell,
		PositionSide: market.Long,
		Type:         market.StopMarket,
		Quantity:     actualQuantity,
		StopPrice:    stopLoss,
	}

	// 손절 주문 실행 전에 로깅 추가
	log.Printf("손절(SL) 주문 생성: 심볼=%s, 가격=%.2f, 수량=%.8f",
		slOrder.Symbol, slOrder.StopPrice, slOrder.Quantity)

	// 손절 주문 실행
	slResponse, err := t.client.PlaceOrder(ctx, slOrder)
	if err != nil {
		log.Printf("손절(SL) 주문 실패: %v", err)
		return fmt.Errorf("손절(SL) 주문 실패: %w", err)
	}
	log.Printf("손절(SL) 주문 성공: ID=%d", slResponse.OrderID)

	// 익절 주문 생성
	tpOrder := market.OrderRequest{
		Symbol:       symbol,
		Side:         market.Sell,
		PositionSide: market.Long,
		Type:         market.TakeProfitMarket,
		Quantity:     actualQuantity,
		StopPrice:    takeProfit,
	}

	// 익절 주문 생성 전에 로깅 추가
	log.Printf("익절(TP) 주문 생성: 심볼=%s, 가격=%.2f, 수량=%.8f",
		tpOrder.Symbol, tpOrder.StopPrice, tpOrder.Quantity)

	// 익절 주문 실행
	tpResponse, err := t.client.PlaceOrder(ctx, tpOrder)
	if err != nil {
		log.Printf("익절(TP) 주문 실패: %v", err)
		return fmt.Errorf("익절(TP) 주문 실패: %w", err)
	}
	log.Printf("익절(TP) 주문 성공: ID=%d", tpResponse.OrderID)

	// 15. TP/SL 설정 완료 알림
	if err := t.discord.SendInfo(fmt.Sprintf("✅ TP/SL 설정 완료: %s", symbol)); err != nil {
		log.Printf("TP/SL 설정 알림 전송 실패: %v", err)
	}

	// TradeInfo 생성 및 전송
	tradeInfo := notification.TradeInfo{
		Symbol:        symbol,
		PositionType:  "LONG",
		PositionValue: positionResult.PositionValue,
		Quantity:      adjustedQuantity,
		EntryPrice:    currentPrice,
		StopLoss:      stopLoss,
		TakeProfit:    takeProfit,
		Balance:       usdtBalance.Available - positionResult.PositionValue,
		Leverage:      leverage,
	}

	if err := t.discord.SendTradeInfo(tradeInfo); err != nil {
		log.Printf("거래 정보 알림 전송 실패: %v", err)
	}

	// 16. 최종 열린 주문 확인
	openOrders, err = t.client.GetOpenOrders(ctx, symbol)
	if err != nil {
		log.Printf("열린 주문 조회 실패: %v", err)
		// 오류가 발생해도 계속 진행
	} else {
		log.Printf("현재 열린 주문 상태 (총 %d개):", len(openOrders))

		var tpCount, slCount int

		for _, order := range openOrders {
			if order.Symbol == symbol && order.PositionSide == "LONG" {
				orderType := ""
				if order.Type == "TAKE_PROFIT_MARKET" {
					orderType = "TP"
					tpCount++
				} else if order.Type == "STOP_MARKET" {
					orderType = "SL"
					slCount++
				}

				if orderType != "" {
					log.Printf("- %s 주문: ID=%d, 가격=%.2f, 수량=%.8f",
						orderType, order.OrderID, order.StopPrice, order.OrigQuantity)
				}
			}
		}

		if tpCount > 0 && slCount > 0 {
			log.Printf("✅ TP/SL 주문이 모두 성공적으로 확인되었습니다!")
			if err := t.discord.SendInfo("✅ 최종 확인: TP/SL 주문이 모두 성공적으로 설정되었습니다!"); err != nil {
				log.Printf("최종 확인 알림 전송 실패: %v", err)
			}
		} else {
			errorMsg := fmt.Sprintf("⚠️ 주의: TP 주문 %d개, SL 주문 %d개 확인됨 (예상: 각 1개)", tpCount, slCount)
			log.Printf(errorMsg)
			if err := t.discord.SendInfo(errorMsg); err != nil {
				log.Printf("주의 알림 전송 실패: %v", err)
			}
		}
	}

	return nil
}

// findBracket은 주어진 레버리지에 해당하는 브라켓을 찾습니다
func findBracket(brackets []market.LeverageBracket, leverage int) *market.LeverageBracket {
	// 레버리지가 높은 순으로 정렬되어 있으므로,
	// 설정된 레버리지보다 크거나 같은 첫 번째 브라켓을 찾습니다.
	for i := len(brackets) - 1; i >= 0; i-- {
		if brackets[i].InitialLeverage >= leverage {
			return &brackets[i]
		}
	}

	// 찾지 못한 경우 가장 낮은 레버리지 브라켓 반환
	if len(brackets) > 0 {
		return &brackets[0]
	}
	return nil
}

// 추가할 AdjustPrice 함수
func AdjustPrice(price float64, tickSize float64, precision int) float64 {
	if tickSize == 0 {
		return price // tickSize가 0이면 조정 불필요
	}

	// tickSize로 나누어 떨어지도록 조정
	ticks := math.Floor(price / tickSize)
	adjustedPrice := ticks * tickSize

	// 정밀도에 맞게 반올림
	scale := math.Pow(10, float64(precision))
	return math.Floor(adjustedPrice*scale) / scale
}

```
## internal/analysis/indicator/ema.go
```go
package indicator

import "fmt"

// EMAOption은 EMA 계산에 필요한 옵션을 정의합니다
type EMAOption struct {
	Period int // 기간
}

// ValidateEMAOption은 EMA 옵션을 검증합니다
func ValidateEMAOption(opt EMAOption) error {
	if opt.Period < 1 {
		return &ValidationError{
			Field: "Period",
			Err:   fmt.Errorf("기간은 1 이상이어야 합니다: %d", opt.Period),
		}
	}
	return nil
}

// EMA는 지수이동평균을 계산합니다
func EMA(prices []PriceData, opt EMAOption) ([]Result, error) {
	if err := ValidateEMAOption(opt); err != nil {
		return nil, err
	}

	if len(prices) == 0 {
		return nil, &ValidationError{
			Field: "prices",
			Err:   fmt.Errorf("가격 데이터가 비어있습니다"),
		}
	}

	if len(prices) < opt.Period {
		return nil, &ValidationError{
			Field: "prices",
			Err:   fmt.Errorf("가격 데이터가 부족합니다. 필요: %d, 현재: %d", opt.Period, len(prices)),
		}
	}

	// EMA 계산을 위한 승수 계산
	multiplier := 2.0 / float64(opt.Period+1)

	results := make([]Result, len(prices))

	// 초기 SMA 계산
	var sma float64
	for i := 0; i < opt.Period; i++ {
		sma += prices[i].Close
	}
	sma /= float64(opt.Period)

	// 첫 번째 EMA는 SMA 값으로 설정
	results[opt.Period-1] = Result{
		Value:     sma,
		Timestamp: prices[opt.Period-1].Time,
	}

	// EMA 계산
	// EMA = 이전 EMA + (현재가 - 이전 EMA) × 승수
	for i := opt.Period; i < len(prices); i++ {
		ema := (prices[i].Close-results[i-1].Value)*multiplier + results[i-1].Value
		results[i] = Result{
			Value:     ema,
			Timestamp: prices[i].Time,
		}
	}

	return results, nil
}

```
## internal/analysis/indicator/indicator_test.go
```go
package indicator

import (
	"fmt"
	"testing"
	"time"
)

// 테스트용 가격 데이터 생성
func generateTestPrices() []PriceData {
	// 2024년 1월 1일부터 시작하는 50일간의 가격 데이터
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	prices := []PriceData{
		// 상승 구간 (변동성 있는)
		{Time: baseTime, Open: 100, High: 105, Low: 98, Close: 103, Volume: 1000},
		{Time: baseTime.AddDate(0, 0, 1), Open: 103, High: 108, Low: 102, Close: 106, Volume: 1200},
		{Time: baseTime.AddDate(0, 0, 2), Open: 106, High: 110, Low: 104, Close: 108, Volume: 1100},
		{Time: baseTime.AddDate(0, 0, 3), Open: 108, High: 112, Low: 107, Close: 110, Volume: 1300},
		{Time: baseTime.AddDate(0, 0, 4), Open: 110, High: 115, Low: 109, Close: 113, Volume: 1400},
		{Time: baseTime.AddDate(0, 0, 5), Open: 113, High: 116, Low: 111, Close: 114, Volume: 1200},
		{Time: baseTime.AddDate(0, 0, 6), Open: 114, High: 118, Low: 113, Close: 116, Volume: 1500},
		{Time: baseTime.AddDate(0, 0, 7), Open: 116, High: 119, Low: 115, Close: 117, Volume: 1300},
		{Time: baseTime.AddDate(0, 0, 8), Open: 117, High: 120, Low: 114, Close: 119, Volume: 1400},
		{Time: baseTime.AddDate(0, 0, 9), Open: 119, High: 122, Low: 118, Close: 120, Volume: 1600},

		// 하락 구간 (급격한 하락)
		{Time: baseTime.AddDate(0, 0, 10), Open: 120, High: 121, Low: 115, Close: 116, Volume: 1800},
		{Time: baseTime.AddDate(0, 0, 11), Open: 116, High: 117, Low: 112, Close: 113, Volume: 2000},
		{Time: baseTime.AddDate(0, 0, 12), Open: 113, High: 115, Low: 108, Close: 109, Volume: 2200},
		{Time: baseTime.AddDate(0, 0, 13), Open: 109, High: 110, Low: 105, Close: 106, Volume: 2400},
		{Time: baseTime.AddDate(0, 0, 14), Open: 106, High: 107, Low: 102, Close: 103, Volume: 2600},

		// 횡보 구간 (변동성 있는)
		{Time: baseTime.AddDate(0, 0, 15), Open: 103, High: 107, Low: 102, Close: 105, Volume: 1400},
		{Time: baseTime.AddDate(0, 0, 16), Open: 105, High: 108, Low: 103, Close: 104, Volume: 1300},
		{Time: baseTime.AddDate(0, 0, 17), Open: 104, High: 106, Low: 102, Close: 106, Volume: 1500},
		{Time: baseTime.AddDate(0, 0, 18), Open: 106, High: 108, Low: 104, Close: 105, Volume: 1400},
		{Time: baseTime.AddDate(0, 0, 19), Open: 105, High: 107, Low: 103, Close: 104, Volume: 1600},

		// 추가 하락 구간
		{Time: baseTime.AddDate(0, 0, 20), Open: 104, High: 105, Low: 100, Close: 101, Volume: 1800},
		{Time: baseTime.AddDate(0, 0, 21), Open: 101, High: 102, Low: 98, Close: 99, Volume: 2000},
		{Time: baseTime.AddDate(0, 0, 22), Open: 99, High: 100, Low: 96, Close: 97, Volume: 2200},
		{Time: baseTime.AddDate(0, 0, 23), Open: 97, High: 98, Low: 94, Close: 95, Volume: 2400},
		{Time: baseTime.AddDate(0, 0, 24), Open: 95, High: 96, Low: 92, Close: 93, Volume: 2600},
		{Time: baseTime.AddDate(0, 0, 25), Open: 93, High: 94, Low: 90, Close: 91, Volume: 2800},

		// 반등 구간
		{Time: baseTime.AddDate(0, 0, 26), Open: 91, High: 96, Low: 91, Close: 95, Volume: 3000},
		{Time: baseTime.AddDate(0, 0, 27), Open: 95, High: 98, Low: 94, Close: 97, Volume: 2800},
		{Time: baseTime.AddDate(0, 0, 28), Open: 97, High: 100, Low: 96, Close: 99, Volume: 2600},
		{Time: baseTime.AddDate(0, 0, 29), Open: 99, High: 102, Low: 98, Close: 101, Volume: 2400},

		// MACD 계산을 위한 추가 데이터
		{Time: baseTime.AddDate(0, 0, 30), Open: 101, High: 104, Low: 100, Close: 103, Volume: 2200},
		{Time: baseTime.AddDate(0, 0, 31), Open: 103, High: 106, Low: 102, Close: 105, Volume: 2000},
		{Time: baseTime.AddDate(0, 0, 32), Open: 105, High: 108, Low: 104, Close: 107, Volume: 1800},
		{Time: baseTime.AddDate(0, 0, 33), Open: 107, High: 110, Low: 106, Close: 109, Volume: 1600},
		{Time: baseTime.AddDate(0, 0, 34), Open: 109, High: 112, Low: 108, Close: 111, Volume: 1400},
	}
	return prices
}

func TestEMA(t *testing.T) {
	prices := generateTestPrices()

	testCases := []struct {
		period int
		name   string
	}{
		{9, "EMA(9)"},
		{12, "EMA(12)"},
		{26, "EMA(26)"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			results, err := EMA(prices, EMAOption{Period: tc.period})
			if err != nil {
				t.Errorf("%s 계산 중 에러 발생: %v", tc.name, err)
				return
			}

			fmt.Printf("\n%s 결과:\n", tc.name)
			for i, result := range results {
				if i < tc.period-1 { // 초기값 건너뜀
					continue
				}
				fmt.Printf("날짜: %s, EMA: %.2f\n",
					result.Timestamp.Format("2006-01-02"), result.Value)

				// EMA 값 검증
				if result.Value <= 0 {
					t.Errorf("잘못된 EMA 값: %.2f", result.Value)
				}
			}
		})
	}
}

func TestMACD(t *testing.T) {
	prices := generateTestPrices()

	macdResults, err := MACD(prices, MACDOption{
		ShortPeriod:  12,
		LongPeriod:   26,
		SignalPeriod: 9,
	})
	if err != nil {
		t.Errorf("MACD 계산 중 에러 발생: %v", err)
		return
	}

	fmt.Println("\nMACD(12,26,9) 결과:")
	for _, result := range macdResults {
		fmt.Printf("날짜: %s, MACD: %.2f, Signal: %.2f, Histogram: %.2f\n",
			result.Timestamp.Format("2006-01-02"),
			result.MACD, result.Signal, result.Histogram)
	}
}

func TestSAR(t *testing.T) {
	prices := generateTestPrices()

	testCases := []struct {
		name string
		opt  SAROption
	}{
		{"기본설정", DefaultSAROption()},
		{"민감설정", SAROption{AccelerationInitial: 0.04, AccelerationMax: 0.4}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			results, err := SAR(prices, tc.opt)
			if err != nil {
				t.Errorf("SAR 계산 중 에러 발생: %v", err)
				return
			}

			fmt.Printf("\nParabolic SAR (%s) 결과:\n", tc.name)
			prevSAR := 0.0
			prevTrend := true
			for i, result := range results {
				fmt.Printf("날짜: %s, SAR: %.2f, 추세: %s\n",
					result.Timestamp.Format("2006-01-02"),
					result.SAR,
					map[bool]string{true: "상승", false: "하락"}[result.IsLong])

				// SAR 값 검증
				if i > 0 {
					// SAR 값이 이전 값과 동일한지 체크
					if result.SAR == prevSAR {
						t.Logf("경고: %s에 SAR 값이 변화없음: %.2f",
							result.Timestamp.Format("2006-01-02"), result.SAR)
					}
					// 추세 전환 확인
					if result.IsLong != prevTrend {
						fmt.Printf(">>> 추세 전환 발생: %s에서 %s로 전환\n",
							map[bool]string{true: "상승", false: "하락"}[prevTrend],
							map[bool]string{true: "상승", false: "하락"}[result.IsLong])
					}
				}
				prevSAR = result.SAR
				prevTrend = result.IsLong
			}
		})
	}
}

func TestAll(t *testing.T) {
	prices := generateTestPrices()

	t.Run("EMA Test", func(t *testing.T) { TestEMA(t) })
	t.Run("MACD Test", func(t *testing.T) { TestMACD(t) })
	t.Run("SAR Test", func(t *testing.T) { TestSAR(t) })

	fmt.Println("\n=== 테스트 데이터 요약 ===")
	fmt.Printf("데이터 기간: %s ~ %s\n",
		prices[0].Time.Format("2006-01-02"),
		prices[len(prices)-1].Time.Format("2006-01-02"))
	fmt.Printf("데이터 개수: %d\n", len(prices))
	fmt.Printf("시작가: %.2f, 종가: %.2f\n", prices[0].Close, prices[len(prices)-1].Close)

	// 데이터 특성 분석
	maxPrice := prices[0].High
	minPrice := prices[0].Low
	maxVolume := prices[0].Volume
	for _, p := range prices {
		if p.High > maxPrice {
			maxPrice = p.High
		}
		if p.Low < minPrice {
			minPrice = p.Low
		}
		if p.Volume > maxVolume {
			maxVolume = p.Volume
		}
	}
	fmt.Printf("가격 범위: %.2f ~ %.2f (변동폭: %.2f%%)\n",
		minPrice, maxPrice, (maxPrice-minPrice)/minPrice*100)
	fmt.Printf("최대 거래량: %.0f\n", maxVolume)
}

```
## internal/analysis/indicator/macd.go
```go
package indicator

import (
	"fmt"
	"time"
)

// MACDOption은 MACD 계산에 필요한 옵션을 정의합니다
type MACDOption struct {
	ShortPeriod  int // 단기 EMA 기간
	LongPeriod   int // 장기 EMA 기간
	SignalPeriod int // 시그널 라인 기간
}

// MACDResult는 MACD 계산 결과를 정의합니다
type MACDResult struct {
	MACD      float64   // MACD 라인
	Signal    float64   // 시그널 라인
	Histogram float64   // 히스토그램
	Timestamp time.Time // 계산 시점
}

// ValidateMACDOption은 MACD 옵션을 검증합니다
func ValidateMACDOption(opt MACDOption) error {
	if opt.ShortPeriod <= 0 {
		return &ValidationError{
			Field: "ShortPeriod",
			Err:   fmt.Errorf("단기 기간은 0보다 커야 합니다: %d", opt.ShortPeriod),
		}
	}
	if opt.LongPeriod <= opt.ShortPeriod {
		return &ValidationError{
			Field: "LongPeriod",
			Err:   fmt.Errorf("장기 기간은 단기 기간보다 커야 합니다: %d <= %d", opt.LongPeriod, opt.ShortPeriod),
		}
	}
	if opt.SignalPeriod <= 0 {
		return &ValidationError{
			Field: "SignalPeriod",
			Err:   fmt.Errorf("시그널 기간은 0보다 커야 합니다: %d", opt.SignalPeriod),
		}
	}
	return nil
}

// MACD는 MACD(Moving Average Convergence Divergence) 지표를 계산합니다
func MACD(prices []PriceData, opt MACDOption) ([]MACDResult, error) {
	if err := ValidateMACDOption(opt); err != nil {
		return nil, err
	}

	// 단기 EMA 계산
	shortEMA, err := EMA(prices, EMAOption{Period: opt.ShortPeriod})
	if err != nil {
		return nil, fmt.Errorf("단기 EMA 계산 실패: %w", err)
	}

	// 장기 EMA 계산
	longEMA, err := EMA(prices, EMAOption{Period: opt.LongPeriod})
	if err != nil {
		return nil, fmt.Errorf("장기 EMA 계산 실패: %w", err)
	}

	// MACD 라인 계산 (단기 EMA - 장기 EMA)
	macdStartIdx := opt.LongPeriod - 1
	macdLine := make([]PriceData, len(prices)-macdStartIdx)
	for i := range macdLine {
		macdLine[i] = PriceData{
			Time:  prices[i+macdStartIdx].Time,
			Close: shortEMA[i+macdStartIdx].Value - longEMA[i+macdStartIdx].Value,
		}
	}

	// 시그널 라인 계산 (MACD의 EMA)
	signalLineData, err := EMA(macdLine, EMAOption{Period: opt.SignalPeriod})
	if err != nil {
		return nil, fmt.Errorf("시그널 라인 계산 실패: %w", err)
	}

	// 최종 결과 생성
	resultStartIdx := opt.SignalPeriod - 1
	results := make([]MACDResult, len(macdLine)-resultStartIdx)
	for i := range results {
		macdIdx := i + resultStartIdx
		results[i] = MACDResult{
			MACD:      macdLine[macdIdx].Close,
			Signal:    signalLineData[i].Value,
			Histogram: macdLine[macdIdx].Close - signalLineData[i].Value,
			Timestamp: macdLine[macdIdx].Time,
		}
	}

	return results, nil
}

```
## internal/analysis/indicator/sar.go
```go
package indicator

import (
	"fmt"
	"math"
	"time"
)

// SAROption은 Parabolic SAR 계산에 필요한 옵션을 정의합니다
type SAROption struct {
	AccelerationInitial float64 // 초기 가속도
	AccelerationMax     float64 // 최대 가속도
}

// DefaultSAROption은 기본 SAR 옵션을 반환합니다
func DefaultSAROption() SAROption {
	return SAROption{
		AccelerationInitial: 0.02,
		AccelerationMax:     0.2,
	}
}

// ValidateSAROption은 SAR 옵션을 검증합니다
func ValidateSAROption(opt SAROption) error {
	if opt.AccelerationInitial <= 0 {
		return &ValidationError{
			Field: "AccelerationInitial",
			Err:   fmt.Errorf("초기 가속도는 0보다 커야 합니다: %f", opt.AccelerationInitial),
		}
	}
	if opt.AccelerationMax <= opt.AccelerationInitial {
		return &ValidationError{
			Field: "AccelerationMax",
			Err: fmt.Errorf("최대 가속도는 초기 가속도보다 커야 합니다: %f <= %f",
				opt.AccelerationMax, opt.AccelerationInitial),
		}
	}
	return nil
}

// SARResult는 Parabolic SAR 계산 결과를 정의합니다
type SARResult struct {
	SAR       float64   // SAR 값
	IsLong    bool      // 현재 추세가 상승인지 여부
	Timestamp time.Time // 계산 시점
}

// SAR은 Parabolic SAR 지표를 계산합니다
func SAR(prices []PriceData, opt SAROption) ([]SARResult, error) {
	results := make([]SARResult, len(prices))
	// 초기값 설정 단순화
	af := opt.AccelerationInitial
	sar := prices[0].Low
	ep := prices[0].High
	isLong := true

	results[0] = SARResult{SAR: sar, IsLong: isLong, Timestamp: prices[0].Time}

	// SAR 계산
	for i := 1; i < len(prices); i++ {
		if isLong {
			sar = sar + af*(ep-sar)

			// 새로운 고점 발견
			if prices[i].High > ep {
				ep = prices[i].High
				af = math.Min(af+opt.AccelerationInitial, opt.AccelerationMax)
			}

			// 추세 전환 체크
			if sar > prices[i].Low {
				isLong = false
				sar = ep
				ep = prices[i].Low
				af = opt.AccelerationInitial
			}
		} else {
			sar = sar - af*(sar-ep)

			// 새로운 저점 발견
			if prices[i].Low < ep {
				ep = prices[i].Low
				af = math.Min(af+opt.AccelerationInitial, opt.AccelerationMax)
			}

			// 추세 전환 체크
			if sar < prices[i].High {
				isLong = true
				sar = ep
				ep = prices[i].High
				af = opt.AccelerationInitial
			}
		}

		results[i] = SARResult{
			SAR:       sar,
			IsLong:    isLong,
			Timestamp: prices[i].Time,
		}
	}

	return results, nil
}

```
## internal/analysis/indicator/types.go
```go
package indicator

import (
	"fmt"
	"time"
)

// PriceData는 지표 계산에 필요한 가격 정보를 정의합니다
type PriceData struct {
	Time   time.Time // 타임스탬프
	Open   float64   // 시가
	High   float64   // 고가
	Low    float64   // 저가
	Close  float64   // 종가
	Volume float64   // 거래량
}

// Result는 지표 계산 결과를 정의합니다
type Result struct {
	Value     float64   // 지표값
	Timestamp time.Time // 계산 시점
}

// ValidationError는 입력값 검증 에러를 정의합니다
type ValidationError struct {
	Field string
	Err   error
}

func (e ValidationError) Error() string {
	return fmt.Sprintf("유효하지 않은 %s: %v", e.Field, e.Err)
}

```
## internal/analysis/signal/detector.go
```go
package signal

import (
	"fmt"
	"log"

	"github.com/assist-by/phoenix/internal/analysis/indicator"
)

// DetectorConfig는 시그널 감지기 설정을 정의합니다
type DetectorConfig struct {
	EMALength      int // EMA 기간 (기본값: 200)
	StopLossPct    float64
	TakeProfitPct  float64
	MinHistogram   float64 // 최소 MACD 히스토그램 값 (기본값: 0.00005)
	MaxWaitCandles int     // 최대 대기 캔들 수 (기본값: 5)
}

// NewDetector는 새로운 시그널 감지기를 생성합니다
func NewDetector(config DetectorConfig) *Detector {
	return &Detector{
		states:         make(map[string]*SymbolState),
		emaLength:      config.EMALength,
		stopLossPct:    config.StopLossPct,
		takeProfitPct:  config.TakeProfitPct,
		minHistogram:   config.MinHistogram,
		maxWaitCandles: config.MaxWaitCandles,
	}
}

// Detect는 주어진 데이터로부터 시그널을 감지합니다
func (d *Detector) Detect(symbol string, prices []indicator.PriceData) (*Signal, error) {
	if len(prices) < d.emaLength {
		return nil, fmt.Errorf("insufficient data: need at least %d prices", d.emaLength)
	}

	// 심볼별 상태 가져오기
	state := d.getSymbolState(symbol)

	// 지표 계산
	ema, err := indicator.EMA(prices, indicator.EMAOption{Period: d.emaLength})
	if err != nil {
		return nil, fmt.Errorf("calculating EMA: %w", err)
	}

	macd, err := indicator.MACD(prices, indicator.MACDOption{
		ShortPeriod:  12,
		LongPeriod:   26,
		SignalPeriod: 9,
	})
	if err != nil {
		return nil, fmt.Errorf("calculating MACD: %w", err)
	}

	sar, err := indicator.SAR(prices, indicator.DefaultSAROption())
	if err != nil {
		return nil, fmt.Errorf("calculating SAR: %w", err)
	}

	currentPrice := prices[len(prices)-1].Close
	currentMACD := macd[len(macd)-1].MACD
	currentSignal := macd[len(macd)-1].Signal
	currentHistogram := currentMACD - currentSignal
	currentEMA := ema[len(ema)-1].Value
	currentSAR := sar[len(sar)-1].SAR
	currentHigh := prices[len(prices)-1].High
	currentLow := prices[len(prices)-1].Low

	/// EMA 및 SAR 조건 확인
	isAboveEMA := currentPrice > currentEMA
	sarBelowCandle := currentSAR < currentLow
	sarAboveCandle := currentSAR > currentHigh

	// MACD 크로스 확인 - 이제 심볼별 상태 사용
	macdCross := d.checkMACDCross(
		currentMACD,
		currentSignal,
		state.PrevMACD,
		state.PrevSignal,
	)

	// 기본 시그널 객체 생성
	signal := &Signal{
		Type:      NoSignal,
		Symbol:    symbol,
		Price:     currentPrice,
		Timestamp: prices[len(prices)-1].Time,
	}

	// 시그널 조건
	signal.Conditions = SignalConditions{
		EMALong:     isAboveEMA,
		EMAShort:    !isAboveEMA,
		MACDLong:    macdCross == 1,
		MACDShort:   macdCross == -1,
		SARLong:     sarBelowCandle,
		SARShort:    !sarBelowCandle,
		EMAValue:    currentEMA,
		MACDValue:   currentMACD,
		SignalValue: currentSignal,
		SARValue:    currentSAR,
	}

	// 1. 대기 상태 확인 및 업데이트
	if state.PendingSignal != NoSignal {
		pendingSignal := d.processPendingState(state, symbol, signal, currentHistogram, sarBelowCandle, sarAboveCandle)
		if pendingSignal != nil {
			// 상태 업데이트
			state.PrevMACD = currentMACD
			state.PrevSignal = currentSignal
			state.PrevHistogram = currentHistogram
			state.LastSignal = pendingSignal
			return pendingSignal, nil
		}
	}

	// 2. 일반 시그널 조건 확인 (대기 상태가 없거나 취소된 경우)

	// Long 시그널
	if isAboveEMA && // EMA 200 위
		macdCross == 1 && // MACD 상향 돌파
		currentHistogram >= d.minHistogram && // MACD 히스토그램이 최소값 이상
		sarBelowCandle { // SAR이 현재 봉의 저가보다 낮음

		signal.Type = Long
		signal.StopLoss = currentSAR                                        // SAR 기반 손절가
		signal.TakeProfit = currentPrice + (currentPrice - signal.StopLoss) // 1:1 비율

		log.Printf("[%s] Long 시그널 감지: 가격=%.2f, EMA200=%.2f, SAR=%.2f",
			symbol, currentPrice, currentEMA, currentSAR)
	}

	// Short 시그널
	if !isAboveEMA && // EMA 200 아래
		macdCross == -1 && // MACD 하향 돌파
		-currentHistogram >= d.minHistogram && // 음수 히스토그램에 대한 조건
		sarAboveCandle { // SAR이 현재 봉의 고가보다 높음

		signal.Type = Short
		signal.StopLoss = currentSAR                                        // SAR 기반 손절가
		signal.TakeProfit = currentPrice - (signal.StopLoss - currentPrice) // 1:1 비율

		log.Printf("[%s] Short 시그널 감지: 가격=%.2f, EMA200=%.2f, SAR=%.2f",
			symbol, currentPrice, currentEMA, currentSAR)
	}

	// 3. 새로운 대기 상태 설정 (일반 시그널이 아닌 경우)
	if signal.Type == NoSignal {
		// MACD 상향돌파 + EMA 위 + SAR 캔들 아래가 아닌 경우 -> 롱 대기 상태
		if isAboveEMA && macdCross == 1 && !sarBelowCandle && currentHistogram > 0 {
			state.PendingSignal = PendingLong
			state.WaitedCandles = 0
			log.Printf("[%s] Long 대기 상태 시작: MACD 상향돌파, SAR 반전 대기", symbol)
		}

		// MACD 하향돌파 + EMA 아래 + SAR이 캔들 위가 아닌 경우 → 숏 대기 상태
		if !isAboveEMA && macdCross == -1 && !sarAboveCandle && currentHistogram < 0 {
			state.PendingSignal = PendingShort
			state.WaitedCandles = 0
			log.Printf("[%s] Short 대기 상태 시작: MACD 하향돌파, SAR 반전 대기", symbol)
		}
	}

	// 상태 업데이트
	state.PrevMACD = currentMACD
	state.PrevSignal = currentSignal
	state.PrevHistogram = currentHistogram

	if signal.Type != NoSignal {
		state.LastSignal = signal
	}

	state.LastSignal = signal
	return signal, nil
}

// processPendingState는 대기 상태를 처리하고 시그널을 생성합니다
func (d *Detector) processPendingState(state *SymbolState, symbol string, baseSignal *Signal, currentHistogram float64, sarBelowCandle bool, sarAboveCandle bool) *Signal {
	// 캔들 카운트 증가
	state.WaitedCandles++

	// 최대 대기 시간 초과 체크
	if state.WaitedCandles > state.MaxWaitCandles {
		log.Printf("[%s] 대기 상태 취소: 최대 대기 캔들 수 (%d) 초과", symbol, state.MaxWaitCandles)
		d.resetPendingState(state)
		return nil
	}

	resultSignal := &Signal{
		Type:       NoSignal,
		Symbol:     baseSignal.Symbol,
		Price:      baseSignal.Price,
		Timestamp:  baseSignal.Timestamp,
		Conditions: baseSignal.Conditions,
	}

	// Long 대기 상태 처리
	if state.PendingSignal == PendingLong {
		// 히스토그램이 음수로 바뀌면 취소(추세 역전)
		if currentHistogram < 0 && state.PrevHistogram > 0 {
			log.Printf("[%s] Long 대기 상태 취소: 히스토그램 부호 변경 (%.5f → %.5f)",
				symbol, state.PrevHistogram, currentHistogram)
			d.resetPendingState(state)
			return nil
		}

		// SAR가 캔들 아래로 이동하면 롱 시그널 생성
		if sarBelowCandle {
			resultSignal.Type = Long
			resultSignal.StopLoss = baseSignal.Conditions.SARValue
			resultSignal.TakeProfit = baseSignal.Price + (baseSignal.Price - resultSignal.StopLoss)

			log.Printf("[%s] Long 대기 상태 → 진입 시그널 전환: %d캔들 대기 후 SAR 반전 확인",
				symbol, state.WaitedCandles)

			d.resetPendingState(state)
			return resultSignal
		}
	}

	// Short 대기 상태 처리
	if state.PendingSignal == PendingShort {
		// 히스토그램이 양수로 바뀌면 취소 (추세 역전)
		if currentHistogram > 0 && state.PrevHistogram < 0 {
			log.Printf("[%s] Short 대기 상태 취소: 히스토그램 부호 변경 (%.5f → %.5f)",
				symbol, state.PrevHistogram, currentHistogram)
			d.resetPendingState(state)
			return nil
		}

		// SAR이 캔들 위로 이동하면 숏 시그널 생성
		if sarAboveCandle {
			resultSignal.Type = Short
			resultSignal.StopLoss = baseSignal.Conditions.SARValue
			resultSignal.TakeProfit = baseSignal.Price - (resultSignal.StopLoss - baseSignal.Price)

			log.Printf("[%s] Short 대기 상태 → 진입 시그널 전환: %d캔들 대기 후 SAR 반전 확인",
				symbol, state.WaitedCandles)

			d.resetPendingState(state)
			return resultSignal
		}
	}

	return nil
}

// checkMACDCross는 MACD 크로스를 확인합니다
// 반환값: 1 (상향돌파), -1 (하향돌파), 0 (크로스 없음)
func (d *Detector) checkMACDCross(currentMACD, currentSignal, prevMACD, prevSignal float64) int {
	if prevMACD <= prevSignal && currentMACD > currentSignal {
		return 1 // 상향돌파
	}
	if prevMACD >= prevSignal && currentMACD < currentSignal {
		return -1 // 하향돌파
	}
	return 0 // 크로스 없음
}

// isDuplicateSignal은 중복 시그널인지 확인합니다
// func (d *Detector) isDuplicateSignal(signal *Signal) bool {
// 	if d.lastSignal == nil {
// 		return false
// 	}

// 	// 동일 방향의 시그널이 이미 존재하는 경우
// 	if d.lastSignal.Type == signal.Type {
// 		return true
// 	}

// 	return false
// }

```
## internal/analysis/signal/signal_test.go
```go
package signal

import (
	"math"
	"testing"
	"time"

	"github.com/assist-by/phoenix/internal/analysis/indicator"
)

// 롱 시그널을 위한 테스트 데이터 생성
func generateLongSignalPrices() []indicator.PriceData {
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	prices := make([]indicator.PriceData, 250) // EMA 200 계산을 위해 충분한 데이터

	// 초기 하락 추세 생성 (0-99)
	startPrice := 100.0
	for i := 0; i < 100; i++ {
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   startPrice - float64(i)*0.1,
			High:   startPrice - float64(i)*0.1 + 0.05,
			Low:    startPrice - float64(i)*0.1 - 0.05,
			Close:  startPrice - float64(i)*0.1,
			Volume: 1000.0,
		}
	}

	// 상승 추세로 전환 (100-199)
	for i := 100; i < 200; i++ {
		increment := float64(i-99) * 0.15
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   startPrice + increment,
			High:   startPrice + increment + 0.05,
			Low:    startPrice + increment - 0.05,
			Close:  startPrice + increment,
			Volume: 1500.0,
		}
	}

	// 마지막 구간에서 강한 상승 추세 (200-249)
	// MACD 골든크로스와 SAR 하향 전환을 만들기 위한 데이터
	for i := 200; i < 250; i++ {
		increment := float64(i-199) * 0.3
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   startPrice + 15.0 + increment,
			High:   startPrice + 15.0 + increment + 0.1,
			Low:    startPrice + 15.0 + increment - 0.05,
			Close:  startPrice + 15.0 + increment + 0.08,
			Volume: 2000.0,
		}
	}

	return prices
}

// 숏 시그널을 위한 테스트 데이터 생성
func generateShortSignalPrices() []indicator.PriceData {
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	prices := make([]indicator.PriceData, 250)

	// 초기 상승 추세 생성 (0-99)
	startPrice := 100.0
	for i := 0; i < 100; i++ {
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   startPrice + float64(i)*0.1,
			High:   startPrice + float64(i)*0.1 + 0.05,
			Low:    startPrice + float64(i)*0.1 - 0.05,
			Close:  startPrice + float64(i)*0.1,
			Volume: 1000.0,
		}
	}

	// 하락 추세로 전환 (100-199)
	for i := 100; i < 200; i++ {
		decrement := float64(i-99) * 0.15
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   startPrice - decrement,
			High:   startPrice - decrement + 0.05,
			Low:    startPrice - decrement - 0.05,
			Close:  startPrice - decrement,
			Volume: 1500.0,
		}
	}

	// 마지막 구간에서 강한 하락 추세 (200-249)
	for i := 200; i < 250; i++ {
		decrement := float64(i-199) * 0.3
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   startPrice - 15.0 - decrement,
			High:   startPrice - 15.0 - decrement + 0.05,
			Low:    startPrice - 15.0 - decrement - 0.1,
			Close:  startPrice - 15.0 - decrement - 0.08,
			Volume: 2000.0,
		}
	}

	return prices
}

// 시그널이 발생하지 않는 데이터 생성
func generateNoSignalPrices() []indicator.PriceData {
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	prices := make([]indicator.PriceData, 250)

	// EMA200 주변에서 횡보하는 데이터 생성
	startPrice := 100.0
	for i := 0; i < 250; i++ {
		// sin 곡선을 사용하여 EMA200 주변에서 진동하는 가격 생성
		cycle := float64(i) * 0.1
		variation := math.Sin(cycle) * 0.5

		price := startPrice + variation
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   price - 0.1,
			High:   price + 0.2,
			Low:    price - 0.2,
			Close:  price,
			Volume: 1000.0 + math.Abs(variation)*100,
		}
	}

	return prices
}

func TestDetector_Detect(t *testing.T) {
	tests := []struct {
		name           string
		prices         []indicator.PriceData
		expectedSignal SignalType
		wantErr        bool
	}{
		{
			name:           "롱 시그널 테스트",
			prices:         generateLongSignalPrices(),
			expectedSignal: Long,
			wantErr:        false,
		},
		{
			name:           "숏 시그널 테스트",
			prices:         generateShortSignalPrices(),
			expectedSignal: Short,
			wantErr:        false,
		},
		{
			name:           "무시그널 테스트",
			prices:         generateNoSignalPrices(),
			expectedSignal: NoSignal,
			wantErr:        false,
		},
		{
			name:           "데이터 부족 테스트",
			prices:         generateLongSignalPrices()[:150], // 200개 미만의 데이터
			expectedSignal: NoSignal,
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			detector := NewDetector(DetectorConfig{
				EMALength:     200,
				StopLossPct:   0.02,
				TakeProfitPct: 0.04,
			})

			signal, err := detector.Detect("BTCUSDT", tt.prices)

			// 에러 검증
			if (err != nil) != tt.wantErr {
				t.Errorf("Detect() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err != nil {
				return
			}

			// 시그널 타입 검증
			if signal.Type != tt.expectedSignal {
				t.Errorf("Expected %v signal, got %v", tt.expectedSignal, signal.Type)
			}

			// 시그널 타입별 추가 검증
			switch signal.Type {
			case Long:
				validateLongSignal(t, signal)
			case Short:
				validateShortSignal(t, signal)
			case NoSignal:
				validateNoSignal(t, signal)
			}
		})
	}
}

func validateLongSignal(t *testing.T, signal *Signal) {
	if !signal.Conditions.EMALong {
		t.Error("Expected price to be above EMA200")
	}
	if !signal.Conditions.MACDLong {
		t.Error("Expected MACD to cross above Signal line")
	}
	if !signal.Conditions.SARLong {
		t.Error("Expected SAR to be below price")
	}
	validateRiskRewardRatio(t, signal)
}

func validateShortSignal(t *testing.T, signal *Signal) {
	if !signal.Conditions.EMAShort {
		t.Error("Expected price to be below EMA200")
	}
	if !signal.Conditions.MACDShort {
		t.Error("Expected MACD to cross below Signal line")
	}
	if !signal.Conditions.SARShort {
		t.Error("Expected SAR to be above price")
	}
	validateRiskRewardRatio(t, signal)
}

func validateNoSignal(t *testing.T, signal *Signal) {
	if signal.StopLoss != 0 || signal.TakeProfit != 0 {
		t.Error("NoSignal should have zero StopLoss and TakeProfit")
	}
}

func validateRiskRewardRatio(t *testing.T, signal *Signal) {
	if signal.StopLoss == 0 || signal.TakeProfit == 0 {
		t.Error("StopLoss and TakeProfit should be non-zero")
		return
	}

	var riskAmount, rewardAmount float64
	if signal.Type == Long {
		riskAmount = signal.Price - signal.StopLoss
		rewardAmount = signal.TakeProfit - signal.Price
	} else {
		riskAmount = signal.StopLoss - signal.Price
		rewardAmount = signal.Price - signal.TakeProfit
	}

	if !almostEqual(riskAmount, rewardAmount, 0.0001) {
		t.Errorf("Risk:Reward ratio is not 1:1 (Risk: %.2f, Reward: %.2f)",
			riskAmount, rewardAmount)
	}
}

// 부동소수점 비교를 위한 헬퍼 함수
func almostEqual(a, b, tolerance float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff <= tolerance
}

// generatePendingLongSignalPrices는 Long 대기 상태를 확인하기 위한 테스트 데이터를 생성합니다
func generatePendingLongSignalPrices() []indicator.PriceData {
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	prices := make([]indicator.PriceData, 250) // EMA 200 계산을 위해 충분한 데이터

	// 초기 하락 추세 생성 (0-99)
	startPrice := 100.0
	for i := 0; i < 100; i++ {
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   startPrice - float64(i)*0.1,
			High:   startPrice - float64(i)*0.1 + 0.05,
			Low:    startPrice - float64(i)*0.1 - 0.05,
			Close:  startPrice - float64(i)*0.1,
			Volume: 1000.0,
		}
	}

	// 상승 추세로 전환 시작 (100-199)
	for i := 100; i < 200; i++ {
		increment := float64(i-99) * 0.15
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   startPrice + increment,
			High:   startPrice + increment + 0.05,
			Low:    startPrice + increment - 0.05,
			Close:  startPrice + increment,
			Volume: 1500.0,
		}
	}

	// 중간에 강한 상승 추세 (200-240) - MACD 골든 크로스 만들기
	for i := 200; i < 240; i++ {
		increment := float64(i-199) * 0.3
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   startPrice + 15.0 + increment,
			High:   startPrice + 15.0 + increment + 0.1,
			Low:    startPrice + 15.0 + increment - 0.05,
			Close:  startPrice + 15.0 + increment + 0.08,
			Volume: 2000.0,
		}
	}

	// 마지막 부분 (240-244)에서 SAR은 아직 캔들 위에 있지만 추세는 지속
	// 이 부분은 대기 상태가 발생하는 구간
	for i := 240; i < 245; i++ {
		increment := float64(i-240) * 0.2
		prices[i] = indicator.PriceData{
			Time: baseTime.Add(time.Hour * time.Duration(i)),
			Open: startPrice + 27.0 + increment,
			// SAR이 계속 캔들 위에 유지되도록 고가를 낮게 설정
			High:   startPrice + 27.0 + increment + 0.03,
			Low:    startPrice + 27.0 + increment - 0.05,
			Close:  startPrice + 27.0 + increment + 0.02,
			Volume: 1800.0,
		}
	}

	// 마지막 캔들 (245-249)에서만 SAR이 캔들 아래로 이동하여 시그널 발생
	for i := 245; i < 250; i++ {
		increment := float64(i-245) * 0.5
		prices[i] = indicator.PriceData{
			Time: baseTime.Add(time.Hour * time.Duration(i)),
			Open: startPrice + 28.0 + increment,
			// 마지막 캔들에서 확실하게 SAR이 아래로 가도록 고가를 확 높게 설정
			High:   startPrice + 28.0 + increment + 1.5,
			Low:    startPrice + 28.0 + increment - 0.1,
			Close:  startPrice + 28.0 + increment + 1.0,
			Volume: 2500.0,
		}
	}

	return prices
}

// generatePendingShortSignalPrices는 Short 대기 상태를 확인하기 위한 테스트 데이터를 생성합니다
func generatePendingShortSignalPrices() []indicator.PriceData {
	baseTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	prices := make([]indicator.PriceData, 250)

	// 초기 상승 추세 생성 (0-99)
	startPrice := 100.0
	for i := 0; i < 100; i++ {
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   startPrice + float64(i)*0.1,
			High:   startPrice + float64(i)*0.1 + 0.05,
			Low:    startPrice + float64(i)*0.1 - 0.05,
			Close:  startPrice + float64(i)*0.1,
			Volume: 1000.0,
		}
	}

	// 하락 추세로 전환 시작 (100-199)
	for i := 100; i < 200; i++ {
		decrement := float64(i-99) * 0.15
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   startPrice - decrement,
			High:   startPrice - decrement + 0.05,
			Low:    startPrice - decrement - 0.05,
			Close:  startPrice - decrement,
			Volume: 1500.0,
		}
	}

	// 강한 하락 추세 (200-240) - MACD 데드 크로스 만들기
	for i := 200; i < 240; i++ {
		decrement := float64(i-199) * 0.3
		prices[i] = indicator.PriceData{
			Time:   baseTime.Add(time.Hour * time.Duration(i)),
			Open:   startPrice - 15.0 - decrement,
			High:   startPrice - 15.0 - decrement + 0.05,
			Low:    startPrice - 15.0 - decrement - 0.1,
			Close:  startPrice - 15.0 - decrement - 0.08,
			Volume: 2000.0,
		}
	}

	// 마지막 부분 (240-244)에서 SAR은 아직 캔들 아래에 있지만 추세는 지속
	// 이 부분은 대기 상태가 발생하는 구간
	for i := 240; i < 245; i++ {
		decrement := float64(i-240) * 0.2
		prices[i] = indicator.PriceData{
			Time: baseTime.Add(time.Hour * time.Duration(i)),
			Open: startPrice - 27.0 - decrement,
			High: startPrice - 27.0 - decrement + 0.03,
			// SAR이 계속 캔들 아래에 유지되도록 저가를 높게 설정
			Low:    startPrice - 27.0 - decrement - 0.03,
			Close:  startPrice - 27.0 - decrement - 0.02,
			Volume: 1800.0,
		}
	}

	// 마지막 캔들 (245-249)에서만 SAR이 캔들 위로 이동하여 시그널 발생
	for i := 245; i < 250; i++ {
		decrement := float64(i-245) * 0.5
		prices[i] = indicator.PriceData{
			Time: baseTime.Add(time.Hour * time.Duration(i)),
			Open: startPrice - 28.0 - decrement,
			High: startPrice - 28.0 - decrement + 0.1,
			// 마지막 캔들에서 확실하게 SAR이 위로 가도록 저가를 확 낮게 설정
			Low:    startPrice - 28.0 - decrement - 1.5,
			Close:  startPrice - 28.0 - decrement - 1.0,
			Volume: 2500.0,
		}
	}

	return prices
}

// TestPendingSignals는 대기 상태 및 신호 감지를 테스트합니다
func TestPendingSignals(t *testing.T) {
	detector := NewDetector(DetectorConfig{
		EMALength:      200,
		StopLossPct:    0.02,
		TakeProfitPct:  0.04,
		MaxWaitCandles: 5,
		MinHistogram:   0.00005,
	})

	t.Run("롱/숏 신호 감지 테스트", func(t *testing.T) {
		// 롱 신호 테스트
		longPrices := generateLongSignalPrices()
		longSignal, err := detector.Detect("BTCUSDT", longPrices)
		if err != nil {
			t.Fatalf("롱 신호 감지 에러: %v", err)
		}

		if longSignal.Type != Long {
			t.Errorf("롱 신호 감지 실패: 예상 타입 Long, 실제 %s", longSignal.Type)
		} else {
			t.Logf("롱 신호 감지 성공: 가격=%.2f, 손절=%.2f, 익절=%.2f",
				longSignal.Price, longSignal.StopLoss, longSignal.TakeProfit)
		}

		// 숏 신호 테스트
		shortPrices := generateShortSignalPrices()
		shortSignal, err := detector.Detect("BTCUSDT", shortPrices)
		if err != nil {
			t.Fatalf("숏 신호 감지 에러: %v", err)
		}

		if shortSignal.Type != Short {
			t.Errorf("숏 신호 감지 실패: 예상 타입 Short, 실제 %s", shortSignal.Type)
		} else {
			t.Logf("숏 신호 감지 성공: 가격=%.2f, 손절=%.2f, 익절=%.2f",
				shortSignal.Price, shortSignal.StopLoss, shortSignal.TakeProfit)
		}
	})

	t.Run("대기 상태 단위 테스트", func(t *testing.T) {
		// 롱 대기 상태 테스트
		symbolState := &SymbolState{
			PendingSignal:  PendingLong,
			WaitedCandles:  2,
			MaxWaitCandles: 5,
			PrevMACD:       0.001,
			PrevSignal:     0.0005,
			PrevHistogram:  0.0005,
		}

		// SAR이 캔들 아래로 반전된 상황 시뮬레이션
		baseSignal := &Signal{
			Type:      NoSignal,
			Symbol:    "BTCUSDT",
			Price:     100.0,
			Timestamp: time.Now(),
			Conditions: SignalConditions{
				SARValue: 98.0, // SAR이 캔들 아래로 이동
			},
		}

		// 롱 대기 상태에서 SAR 반전 시 롱 신호가 생성되는지 확인
		result := detector.processPendingState(symbolState, "BTCUSDT", baseSignal, 0.001, true, false)
		if result == nil {
			t.Errorf("롱 대기 상태에서 SAR 반전 시 신호가 생성되지 않음")
		} else if result.Type != Long {
			t.Errorf("롱 대기 상태에서 SAR 반전 시 잘못된 신호 타입: %s", result.Type)
		} else {
			t.Logf("롱 대기 상태 테스트 성공")
		}

		// 숏 대기 상태 테스트
		symbolState = &SymbolState{
			PendingSignal:  PendingShort,
			WaitedCandles:  2,
			MaxWaitCandles: 5,
			PrevMACD:       -0.001,
			PrevSignal:     -0.0005,
			PrevHistogram:  -0.0005,
		}

		// SAR이 캔들 위로 반전된 상황 시뮬레이션
		baseSignal = &Signal{
			Type:      NoSignal,
			Symbol:    "BTCUSDT",
			Price:     100.0,
			Timestamp: time.Now(),
			Conditions: SignalConditions{
				SARValue: 102.0, // SAR이 캔들 위로 이동
			},
		}

		// 숏 대기 상태에서 SAR 반전 시 숏 신호가 생성되는지 확인
		result = detector.processPendingState(symbolState, "BTCUSDT", baseSignal, -0.001, false, true)
		if result == nil {
			t.Errorf("숏 대기 상태에서 SAR 반전 시 신호가 생성되지 않음")
		} else if result.Type != Short {
			t.Errorf("숏 대기 상태에서 SAR 반전 시 잘못된 신호 타입: %s", result.Type)
		} else {
			t.Logf("숏 대기 상태 테스트 성공")
		}
	})
}

```
## internal/analysis/signal/state.go
```go
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

```
## internal/analysis/signal/types.go
```go
package signal

import (
	"sync"
	"time"
)

// SignalType은 시그널 유형을 정의합니다
type SignalType int

const (
	NoSignal SignalType = iota
	Long
	Short
	PendingLong  // MACD 상향 돌파 후 SAR 반전 대기 상태
	PendingShort // MACD 하향돌파 후 SAR 반전 대기 상태
)

func (s SignalType) String() string {
	switch s {
	case NoSignal:
		return "NoSignal"
	case Long:
		return "Long"
	case Short:
		return "Short"
	case PendingLong:
		return "PendingLong"
	case PendingShort:
		return "PendingShort"
	default:
		return "Unknown"
	}
}

// SignalConditions는 시그널 발생 조건들의 상세 정보를 저장합니다
type SignalConditions struct {
	EMALong     bool    // 가격이 EMA 위
	EMAShort    bool    // 가격이 EMA 아래
	MACDLong    bool    // MACD 상향돌파
	MACDShort   bool    // MACD 하향돌파
	SARLong     bool    // SAR이 가격 아래
	SARShort    bool    // SAR이 가격 위
	EMAValue    float64 // EMA 값
	MACDValue   float64 // MACD 값
	SignalValue float64 // MACD Signal 값
	SARValue    float64 // SAR 값
}

// Signal은 생성된 시그널 정보를 담습니다
type Signal struct {
	Type       SignalType
	Symbol     string
	Price      float64
	Timestamp  time.Time
	Conditions SignalConditions
	StopLoss   float64
	TakeProfit float64
}

// SymbolState는 각 심볼별 상태를 관리합니다
type SymbolState struct {
	PrevMACD       float64    // 이전 MACD 값
	PrevSignal     float64    // 이전 Signal 값
	PrevHistogram  float64    // 이전 히스토그램 값
	LastSignal     *Signal    // 마지막 발생 시그널
	PendingSignal  SignalType // 대기중인 시그널 타입
	WaitedCandles  int        // 대기한 캔들 수
	MaxWaitCandles int        // 최대 대기 캔들 수
}

// Detector는 시그널 감지기를 정의합니다
type Detector struct {
	states         map[string]*SymbolState
	emaLength      int     // EMA 기간
	stopLossPct    float64 // 손절 비율
	takeProfitPct  float64 // 익절 비율
	minHistogram   float64 // MACD 히스토그램 최소값
	maxWaitCandles int     // 기본 최대 대기 캔들 수
	mu             sync.RWMutex
}

```
## internal/config/config.go
```go
package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	// 바이낸스 API 설정
	Binance struct {
		// 메인넷 API 키
		APIKey    string `envconfig:"BINANCE_API_KEY" required:"true"`
		SecretKey string `envconfig:"BINANCE_SECRET_KEY" required:"true"`

		// 테스트넷 API 키
		TestAPIKey    string `envconfig:"BINANCE_TEST_API_KEY" required:"false"`
		TestSecretKey string `envconfig:"BINANCE_TEST_SECRET_KEY" required:"false"`

		// 테스트넷 사용 여부
		UseTestnet bool `envconfig:"USE_TESTNET" default:"false"`
	}

	// 디스코드 웹훅 설정
	Discord struct {
		SignalWebhook string `envconfig:"DISCORD_SIGNAL_WEBHOOK" required:"true"`
		TradeWebhook  string `envconfig:"DISCORD_TRADE_WEBHOOK" required:"true"`
		ErrorWebhook  string `envconfig:"DISCORD_ERROR_WEBHOOK" required:"true"`
		InfoWebhook   string `envconfig:"DISCORD_INFO_WEBHOOK" required:"true"`
	}

	// 애플리케이션 설정
	App struct {
		FetchInterval   time.Duration `envconfig:"FETCH_INTERVAL" default:"15m"`
		CandleLimit     int           `envconfig:"CANDLE_LIMIT" default:"100"`
		Symbols         []string      `envconfig:"SYMBOLS" default:""`              // 커스텀 심볼 목록
		UseTopSymbols   bool          `envconfig:"USE_TOP_SYMBOLS" default:"false"` // 거래량 상위 심볼 사용 여부
		TopSymbolsCount int           `envconfig:"TOP_SYMBOLS_COUNT" default:"3"`   // 거래량 상위 심볼 개수
	}

	// 거래 설정
	Trading struct {
		Leverage int `envconfig:"LEVERAGE" default:"5" validate:"min=1,max=100"`
	}
}

// ValidateConfig는 설정이 유효한지 확인합니다.
func ValidateConfig(cfg *Config) error {
	if cfg.Binance.UseTestnet {
		// 테스트넷 모드일 때 테스트넷 API 키 검증
		if cfg.Binance.TestAPIKey == "" || cfg.Binance.TestSecretKey == "" {
			return fmt.Errorf("테스트넷 모드에서는 BINANCE_TEST_API_KEY와 BINANCE_TEST_SECRET_KEY가 필요합니다")
		}
	} else {
		// 메인넷 모드일 때 메인넷 API 키 검증
		if cfg.Binance.APIKey == "" || cfg.Binance.SecretKey == "" {
			return fmt.Errorf("메인넷 모드에서는 BINANCE_API_KEY와 BINANCE_SECRET_KEY가 필요합니다")
		}
	}

	if cfg.Trading.Leverage < 1 || cfg.Trading.Leverage > 100 {
		return fmt.Errorf("레버리지는 1 이상 100 이하이어야 합니다")
	}

	if cfg.App.FetchInterval < 1*time.Minute {
		return fmt.Errorf("FETCH_INTERVAL은 1분 이상이어야 합니다")
	}

	if cfg.App.CandleLimit < 300 {
		return fmt.Errorf("CANDLE_LIMIT은 300 이상이어야 합니다")
	}

	return nil
}

// LoadConfig는 환경변수에서 설정을 로드합니다.
func LoadConfig() (*Config, error) {
	// .env 파일 로드
	if err := godotenv.Load(); err != nil {
		return nil, fmt.Errorf(".env 파일 로드 실패: %w", err)
	}

	var cfg Config
	// 환경변수를 구조체로 파싱
	if err := envconfig.Process("", &cfg); err != nil {
		return nil, fmt.Errorf("환경변수 처리 실패: %w", err)
	}

	// 심볼 문자열 파싱
	if symbolsStr := os.Getenv("SYMBOLS"); symbolsStr != "" {
		cfg.App.Symbols = strings.Split(symbolsStr, ",")
		for i, s := range cfg.App.Symbols {
			cfg.App.Symbols[i] = strings.TrimSpace(s)
		}
	}

	// 설정값 검증
	if err := ValidateConfig(&cfg); err != nil {
		return nil, fmt.Errorf("설정값 검증 실패: %w", err)
	}

	return &cfg, nil
}

```
## internal/market/client.go
```go
package market

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Client는 바이낸스 API 클라이언트를 구현합니다
type Client struct {
	apiKey           string
	secretKey        string
	baseURL          string
	httpClient       *http.Client
	serverTimeOffset int64 // 서버 시간과의 차이를 저장
	mu               sync.RWMutex
}

// ClientOption은 클라이언트 생성 옵션을 정의합니다
type ClientOption func(*Client)

// WithTestnet은 테스트넷 사용 여부를 설정합니다
func WithTestnet(useTestnet bool) ClientOption {
	return func(c *Client) {
		if useTestnet {
			c.baseURL = "https://testnet.binancefuture.com"
		} else {
			c.baseURL = "https://fapi.binance.com"
		}
	}
}

// WithTimeout은 HTTP 클라이언트의 타임아웃을 설정합니다
func WithTimeout(timeout time.Duration) ClientOption {
	return func(c *Client) {
		c.httpClient.Timeout = timeout
	}
}

// WithBaseURL은 기본 URL을 설정합니다
func WithBaseURL(baseURL string) ClientOption {
	return func(c *Client) {
		c.baseURL = baseURL
	}
}

// NewClient는 새로운 바이낸스 API 클라이언트를 생성합니다
func NewClient(apiKey, secretKey string, opts ...ClientOption) *Client {
	c := &Client{
		apiKey:     apiKey,
		secretKey:  secretKey,
		baseURL:    "https://fapi.binance.com", // 기본값은 선물 거래소
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}

	// 옵션 적용
	for _, opt := range opts {
		opt(c)
	}

	return c
}

// sign은 요청에 대한 서명을 생성합니다
func (c *Client) sign(payload string) string {
	h := hmac.New(sha256.New, []byte(c.secretKey))
	h.Write([]byte(payload))
	return hex.EncodeToString(h.Sum(nil))
}

// doRequest는 HTTP 요청을 실행하고 결과를 반환합니다
func (c *Client) doRequest(ctx context.Context, method, endpoint string, params url.Values, needSign bool) ([]byte, error) {
	if params == nil {
		params = url.Values{}
	}

	// URL 생성
	reqURL, err := url.Parse(c.baseURL + endpoint)
	if err != nil {
		return nil, fmt.Errorf("URL 파싱 실패: %w", err)
	}

	// 타임스탬프 추가
	if needSign {
		// 서버 시간으로 타임스탬프 설정
		timestamp := strconv.FormatInt(c.getServerTime(), 10)
		params.Set("timestamp", timestamp)
		// recvWindow 설정 (선택적)
		params.Set("recvWindow", "5000")
	}

	// 파라미터 설정
	reqURL.RawQuery = params.Encode()

	// 서명 추가
	if needSign {
		signature := c.sign(params.Encode())
		reqURL.RawQuery = reqURL.RawQuery + "&signature=" + signature
	}

	// 요청 생성
	req, err := http.NewRequestWithContext(ctx, method, reqURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("요청 생성 실패: %w", err)
	}

	// 헤더 설정
	req.Header.Set("Content-Type", "application/json")
	if needSign {
		req.Header.Set("X-MBX-APIKEY", c.apiKey)
	}

	// 요청 실행
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("API 요청 실패: %w", err)
	}
	defer resp.Body.Close()

	// 응답 읽기
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("응답 읽기 실패: %w", err)
	}

	// 상태 코드 확인
	if resp.StatusCode != http.StatusOK {
		var apiErr struct {
			Code    int    `json:"code"`
			Message string `json:"msg"`
		}
		if err := json.Unmarshal(body, &apiErr); err != nil {
			return nil, fmt.Errorf("HTTP 에러(%d): %s", resp.StatusCode, string(body))
		}
		return nil, fmt.Errorf("API 에러(코드: %d): %s", apiErr.Code, apiErr.Message)
	}

	return body, nil
}

// GetServerTime은 서버 시간을 조회합니다
func (c *Client) GetServerTime(ctx context.Context) (time.Time, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/fapi/v1/time", nil, false)
	if err != nil {
		return time.Time{}, err
	}

	var result struct {
		ServerTime int64 `json:"serverTime"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return time.Time{}, fmt.Errorf("서버 시간 파싱 실패: %w", err)
	}

	return time.Unix(0, result.ServerTime*int64(time.Millisecond)), nil
}

// GetKlines는 캔들 데이터를 조회합니다
func (c *Client) GetKlines(ctx context.Context, symbol, interval string, limit int) ([]CandleData, error) {
	params := url.Values{}
	params.Add("symbol", symbol)
	params.Add("interval", interval)
	params.Add("limit", strconv.Itoa(limit))

	resp, err := c.doRequest(ctx, http.MethodGet, "/fapi/v1/klines", params, false)
	if err != nil {
		return nil, err
	}

	var rawCandles [][]interface{}
	if err := json.Unmarshal(resp, &rawCandles); err != nil {
		return nil, fmt.Errorf("캔들 데이터 파싱 실패: %w", err)
	}

	candles := make([]CandleData, len(rawCandles))
	for i, raw := range rawCandles {
		candles[i] = CandleData{
			OpenTime:  int64(raw[0].(float64)),
			CloseTime: int64(raw[6].(float64)),
		}
		// 숫자 문자열을 float64로 변환
		if open, err := strconv.ParseFloat(raw[1].(string), 64); err == nil {
			candles[i].Open = open
		}
		if high, err := strconv.ParseFloat(raw[2].(string), 64); err == nil {
			candles[i].High = high
		}
		if low, err := strconv.ParseFloat(raw[3].(string), 64); err == nil {
			candles[i].Low = low
		}
		if close, err := strconv.ParseFloat(raw[4].(string), 64); err == nil {
			candles[i].Close = close
		}
		if volume, err := strconv.ParseFloat(raw[5].(string), 64); err == nil {
			candles[i].Volume = volume
		}
	}

	return candles, nil
}

// GetBalance는 계정의 잔고를 조회합니다
func (c *Client) GetBalance(ctx context.Context) (map[string]Balance, error) {
	params := url.Values{}

	resp, err := c.doRequest(ctx, http.MethodGet, "/fapi/v2/account", params, true)
	if err != nil {
		return nil, fmt.Errorf("잔고 조회 실패: %w", err)
	}

	var result struct {
		Assets []struct {
			Asset            string  `json:"asset"`
			WalletBalance    float64 `json:"walletBalance,string"`
			UnrealizedProfit float64 `json:"unrealizedProfit,string"`
			MarginBalance    float64 `json:"marginBalance,string"`
			AvailableBalance float64 `json:"availableBalance,string"`
		} `json:"assets"`
	}

	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("응답 파싱 실패: %w", err)
	}

	balances := make(map[string]Balance)
	for _, asset := range result.Assets {
		if asset.WalletBalance > 0 {
			balances[asset.Asset] = Balance{
				Asset:              asset.Asset,
				Available:          asset.AvailableBalance,
				Locked:             asset.WalletBalance - asset.AvailableBalance,
				CrossWalletBalance: asset.WalletBalance,
			}
		}
	}

	return balances, nil
}

// PlaceOrder는 새로운 주문을 생성합니다
// PlaceOrder는 새로운 주문을 생성합니다
func (c *Client) PlaceOrder(ctx context.Context, order OrderRequest) (*OrderResponse, error) {
	params := url.Values{}
	params.Add("symbol", order.Symbol)
	params.Add("side", string(order.Side))

	if order.PositionSide != "" {
		params.Add("positionSide", string(order.PositionSide))
	}

	var endpoint string = "/fapi/v1/order"

	switch order.Type {
	case Market:
		params.Add("type", "MARKET")
		if order.QuoteOrderQty > 0 {
			// USDT 금액으로 주문 (추가된 부분)
			params.Add("quoteOrderQty", strconv.FormatFloat(order.QuoteOrderQty, 'f', -1, 64))
		} else {
			// 기존 방식 (코인 수량으로 주문)
			params.Add("quantity", strconv.FormatFloat(order.Quantity, 'f', -1, 64))
		}

	case Limit:
		params.Add("type", "LIMIT")
		params.Add("timeInForce", "GTC")
		params.Add("quantity", strconv.FormatFloat(order.Quantity, 'f', -1, 64))
		params.Add("price", strconv.FormatFloat(order.Price, 'f', -1, 64))

	case StopMarket:
		params.Add("type", "STOP_MARKET")
		params.Add("quantity", strconv.FormatFloat(order.Quantity, 'f', -1, 64))
		params.Add("stopPrice", strconv.FormatFloat(order.StopPrice, 'f', -1, 64))

	case TakeProfitMarket:
		params.Add("type", "TAKE_PROFIT_MARKET")
		params.Add("quantity", strconv.FormatFloat(order.Quantity, 'f', -1, 64))
		params.Add("stopPrice", strconv.FormatFloat(order.StopPrice, 'f', -1, 64))
	}

	resp, err := c.doRequest(ctx, http.MethodPost, endpoint, params, true)
	if err != nil {
		return nil, fmt.Errorf("주문 실행 실패: %w", err)
	}

	var result OrderResponse
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("주문 응답 파싱 실패: %w", err)
	}

	return &result, nil
}

// GetSymbolInfo는 특정 심볼의 거래 정보만 조회합니다
func (c *Client) GetSymbolInfo(ctx context.Context, symbol string) (*SymbolInfo, error) {
	// 요청 파라미터에 심볼 추가
	params := url.Values{}
	params.Add("symbol", symbol)

	// 특정 심볼에 대한 exchangeInfo 호출
	resp, err := c.doRequest(ctx, http.MethodGet, "/fapi/v1/exchangeInfo", params, false)
	if err != nil {
		return nil, fmt.Errorf("심볼 정보 조회 실패: %w", err)
	}

	// exchangeInfo 응답 구조체 정의
	var exchangeInfo struct {
		Symbols []struct {
			Symbol            string `json:"symbol"`
			PricePrecision    int    `json:"pricePrecision"`
			QuantityPrecision int    `json:"quantityPrecision"`
			Filters           []struct {
				FilterType  string `json:"filterType"`
				StepSize    string `json:"stepSize,omitempty"`
				TickSize    string `json:"tickSize,omitempty"`
				MinNotional string `json:"notional,omitempty"`
			} `json:"filters"`
		} `json:"symbols"`
	}

	// JSON 응답 파싱
	if err := json.Unmarshal(resp, &exchangeInfo); err != nil {
		return nil, fmt.Errorf("심볼 정보 파싱 실패: %w", err)
	}

	// 응답에 심볼 정보가 없는 경우
	if len(exchangeInfo.Symbols) == 0 {
		return nil, fmt.Errorf("심볼 정보를 찾을 수 없음: %s", symbol)
	}

	// 첫 번째(유일한) 심볼 정보 사용
	s := exchangeInfo.Symbols[0]

	info := &SymbolInfo{
		Symbol:            symbol,
		PricePrecision:    s.PricePrecision,
		QuantityPrecision: s.QuantityPrecision,
	}

	// 필터 정보 추출
	for _, filter := range s.Filters {
		switch filter.FilterType {
		case "LOT_SIZE": // 수량 단위 필터
			if filter.StepSize != "" {
				stepSize, err := strconv.ParseFloat(filter.StepSize, 64)
				if err != nil {
					log.Printf("LOT_SIZE 파싱 오류: %v", err)
					continue
				}
				info.StepSize = stepSize
			}
		case "PRICE_FILTER": // 가격 단위 필터
			if filter.TickSize != "" {
				tickSize, err := strconv.ParseFloat(filter.TickSize, 64)
				if err != nil {
					log.Printf("PRICE_FILTER 파싱 오류: %v", err)
					continue
				}
				info.TickSize = tickSize
			}
		case "MIN_NOTIONAL": // 최소 주문 가치 필터
			if filter.MinNotional != "" {
				minNotional, err := strconv.ParseFloat(filter.MinNotional, 64)
				if err != nil {
					log.Printf("MIN_NOTIONAL 파싱 오류: %v", err)
					continue
				}
				info.MinNotional = minNotional
			}
		}
	}

	// 정보 로깅
	log.Printf("심볼 정보 조회: %s (최소단위: %.8f, 가격단위: %.8f, 최소주문가치: %.2f)",
		info.Symbol, info.StepSize, info.TickSize, info.MinNotional)

	return info, nil
}

// SetLeverage는 레버리지를 설정합니다
func (c *Client) SetLeverage(ctx context.Context, symbol string, leverage int) error {
	params := url.Values{}
	params.Add("symbol", symbol)
	params.Add("leverage", strconv.Itoa(leverage))

	_, err := c.doRequest(ctx, http.MethodPost, "/fapi/v1/leverage", params, true)
	if err != nil {
		return fmt.Errorf("레버리지 설정 실패: %w", err)
	}

	return nil
}

// SetPositionMode는 포지션 모드를 설정합니다
func (c *Client) SetPositionMode(ctx context.Context, hedgeMode bool) error {
	params := url.Values{}
	params.Add("dualSidePosition", strconv.FormatBool(hedgeMode))

	_, err := c.doRequest(ctx, http.MethodPost, "/fapi/v1/positionSide/dual", params, true)
	if err != nil {
		// API 에러 타입 확인
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Code == ErrPositionModeNoChange {
			return nil // 이미 원하는 모드로 설정된 경우
		}
		// 문자열 검사 추가
		if strings.Contains(err.Error(), "No need to change position side") {
			return nil
		}
		return fmt.Errorf("포지션 모드 설정 실패: %w", err)
	}
	return nil
}

// getOppositeOrderSide는 주문의 반대 방향을 반환합니다
func getOppositeOrderSide(side OrderSide) OrderSide {
	if side == Buy {
		return Sell
	}
	return Buy
}

// GetTopVolumeSymbols는 거래량 기준 상위 n개 심볼을 조회합니다
func (c *Client) GetTopVolumeSymbols(ctx context.Context, n int) ([]string, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/fapi/v1/ticker/24hr", nil, false)
	if err != nil {
		return nil, fmt.Errorf("거래량 데이터 조회 실패: %w", err)
	}

	var tickers []SymbolVolume
	if err := json.Unmarshal(resp, &tickers); err != nil {
		return nil, fmt.Errorf("거래량 데이터 파싱 실패: %w", err)
	}

	// USDT 마진 선물만 필터링
	var filteredTickers []SymbolVolume
	for _, ticker := range tickers {
		if strings.HasSuffix(ticker.Symbol, "USDT") {
			filteredTickers = append(filteredTickers, ticker)
		}
	}

	// 거래량 기준 내림차순 정렬
	sort.Slice(filteredTickers, func(i, j int) bool {
		return filteredTickers[i].QuoteVolume > filteredTickers[j].QuoteVolume
	})

	// 상위 n개 심볼 선택
	resultCount := min(n, len(filteredTickers))
	symbols := make([]string, resultCount)
	for i := 0; i < resultCount; i++ {
		symbols[i] = filteredTickers[i].Symbol
	}

	// 거래량 로깅 (시각화)
	if len(filteredTickers) > 0 {
		maxVolume := filteredTickers[0].QuoteVolume
		log.Println("\n=== 상위 거래량 심볼 ===")
		for i := 0; i < resultCount; i++ {
			ticker := filteredTickers[i]
			barLength := int((ticker.QuoteVolume / maxVolume) * 50) // 최대 50칸
			bar := strings.Repeat("=", barLength)
			log.Printf("%-12s %15.2f USDT ||%s\n",
				ticker.Symbol, ticker.QuoteVolume, bar)
		}
		log.Println("========================")
	}

	return symbols, nil
}

// GetPositions는 현재 열림 포지션을 조회합니다
func (c *Client) GetPositions(ctx context.Context) ([]PositionInfo, error) {
	params := url.Values{}

	resp, err := c.doRequest(ctx, http.MethodGet, "/fapi/v2/positionRisk", params, true)
	if err != nil {
		return nil, fmt.Errorf("포지션 조회 실패: %w", err)
	}

	var positions []PositionInfo
	if err := json.Unmarshal(resp, &positions); err != nil {
		return nil, fmt.Errorf("포지션 데이터 파싱 실패: %w", err)
	}

	activePositions := []PositionInfo{}
	for _, p := range positions {
		if p.Quantity != 0 {
			activePositions = append(activePositions, p)
		}
	}
	return activePositions, nil
}

// GetLeverageBrackets는 심볼의 레버리지 브라켓 정보를 조회합니다
func (c *Client) GetLeverageBrackets(ctx context.Context, symbol string) ([]SymbolBrackets, error) {
	params := url.Values{}
	if symbol != "" {
		params.Add("symbol", symbol)
	}

	resp, err := c.doRequest(ctx, http.MethodGet, "/fapi/v1/leverageBracket", params, true)
	if err != nil {
		return nil, fmt.Errorf("레버리지 브라켓 조회 실패: %w", err)
	}

	var brackets []SymbolBrackets
	if err := json.Unmarshal(resp, &brackets); err != nil {
		return nil, fmt.Errorf("레버리지 브라켓 데이터 파싱 실패: %w", err)
	}

	return brackets, nil
}

// =================================
// 시간 관련된 함수

// SyncTime은 바이낸스 서버와 시간을 동기화합니다
func (c *Client) SyncTime(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	resp, err := c.doRequest(ctx, http.MethodGet, "/fapi/v1/time", nil, false)
	if err != nil {
		return fmt.Errorf("서버 시간 조회 실패: %w", err)
	}

	var result struct {
		ServerTime int64 `json:"serverTime"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return fmt.Errorf("서버 시간 파싱 실패: %w", err)
	}

	c.serverTimeOffset = result.ServerTime - time.Now().UnixMilli()
	return nil
}

// getServerTime은 현재 서버 시간을 반환합니다
func (c *Client) getServerTime() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return time.Now().UnixMilli() + c.serverTimeOffset
}

// GetOpenOrders는 현재 열린 주문 목록을 조회합니다
func (c *Client) GetOpenOrders(ctx context.Context, symbol string) ([]OrderInfo, error) {
	params := url.Values{}
	if symbol != "" {
		params.Add("symbol", symbol)
	}

	resp, err := c.doRequest(ctx, http.MethodGet, "/fapi/v1/openOrders", params, true)
	if err != nil {
		return nil, fmt.Errorf("열린 주문 조회 실패: %w", err)
	}

	var orders []OrderInfo
	if err := json.Unmarshal(resp, &orders); err != nil {
		return nil, fmt.Errorf("주문 데이터 파싱 실패: %w", err)
	}

	return orders, nil
}

// CancelOrder는 주문을 취소합니다
func (c *Client) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	params := url.Values{}
	params.Add("symbol", symbol)
	params.Add("orderId", strconv.FormatInt(orderID, 10))

	_, err := c.doRequest(ctx, http.MethodDelete, "/fapi/v1/order", params, true)
	if err != nil {
		return fmt.Errorf("주문 취소 실패: %w", err)
	}

	return nil
}

```
## internal/market/collector.go
```go
package market

import (
	"context"
	"fmt"
	"log"
	"math"
	"sync"
	"time"

	"github.com/assist-by/phoenix/internal/analysis/indicator"
	"github.com/assist-by/phoenix/internal/analysis/signal"
	"github.com/assist-by/phoenix/internal/config"
	"github.com/assist-by/phoenix/internal/notification"
	"github.com/assist-by/phoenix/internal/notification/discord"
)

// RetryConfig는 재시도 설정을 정의합니다
type RetryConfig struct {
	MaxRetries int           // 최대 재시도 횟수
	BaseDelay  time.Duration // 기본 대기 시간
	MaxDelay   time.Duration // 최대 대기 시간
	Factor     float64       // 대기 시간 증가 계수
}

// Collector는 시장 데이터 수집기를 구현합니다
type Collector struct {
	client   *Client
	discord  *discord.Client
	detector *signal.Detector
	config   *config.Config

	retry RetryConfig
	mu    sync.Mutex // RWMutex에서 일반 Mutex로 변경
}

// NewCollector는 새로운 데이터 수집기를 생성합니다
func NewCollector(client *Client, discord *discord.Client, detector *signal.Detector, config *config.Config, opts ...CollectorOption) *Collector {
	c := &Collector{
		client:   client,
		discord:  discord,
		detector: detector,
		config:   config,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// CollectorOption은 수집기의 옵션을 정의합니다
type CollectorOption func(*Collector)

// WithCandleLimit은 캔들 데이터 조회 개수를 설정합니다
func WithCandleLimit(limit int) CollectorOption {
	return func(c *Collector) {
		c.config.App.CandleLimit = limit
	}
}

// WithRetryConfig는 재시도 설정을 지정합니다
func WithRetryConfig(config RetryConfig) CollectorOption {
	return func(c *Collector) {
		c.retry = config
	}
}

// collect는 한 번의 데이터 수집 사이클을 수행합니다
func (c *Collector) Collect(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 심볼 목록 결정
	var symbols []string
	var err error

	if c.config.App.UseTopSymbols {
		// 거래량 상위 심볼 조회
		err = c.withRetry(ctx, "상위 거래량 심볼 조회", func() error {
			var err error
			symbols, err = c.client.GetTopVolumeSymbols(ctx, c.config.App.TopSymbolsCount)
			return err
		})
		if err != nil {
			return fmt.Errorf("상위 거래량 심볼 조회 실패: %w", err)
		}
	} else {
		// 설정된 심볼 사용
		if len(c.config.App.Symbols) > 0 {
			symbols = c.config.App.Symbols
		} else {
			// 기본값으로 BTCUSDT 사용
			symbols = []string{"BTCUSDT"}
		}
	}

	// 각 심볼의 잔고 조회
	var balances map[string]Balance
	err = c.withRetry(ctx, "잔고 조회", func() error {
		var err error
		balances, err = c.client.GetBalance(ctx)
		return err
	})
	if err != nil {
		return err
	}

	// 잔고 정보 로깅 및 알림
	balanceInfo := "현재 보유 잔고:\n"
	for asset, balance := range balances {
		if balance.Available > 0 || balance.Locked > 0 {
			balanceInfo += fmt.Sprintf("%s: 총: %.8f, 사용가능: %.8f, 잠금: %.8f\n",
				asset, balance.CrossWalletBalance, balance.Available, balance.Locked)
		}
	}
	if c.discord != nil {
		if err := c.discord.SendInfo(balanceInfo); err != nil {
			log.Printf("잔고 정보 알림 전송 실패: %v", err)
		}
	}

	// 각 심볼의 캔들 데이터 수집
	for _, symbol := range symbols {
		err := c.withRetry(ctx, fmt.Sprintf("%s 캔들 데이터 조회", symbol), func() error {
			candles, err := c.client.GetKlines(ctx, symbol, c.getIntervalString(), c.config.App.CandleLimit)
			if err != nil {
				return err
			}

			log.Printf("%s 심볼의 캔들 데이터 %d개 수집 완료", symbol, len(candles))

			// 캔들 데이터를 indicator.PriceData로 변환
			prices := make([]indicator.PriceData, len(candles))
			for i, candle := range candles {
				prices[i] = indicator.PriceData{
					Time:   time.Unix(candle.OpenTime/1000, 0),
					Open:   candle.Open,
					High:   candle.High,
					Low:    candle.Low,
					Close:  candle.Close,
					Volume: candle.Volume,
				}
			}

			// 시그널 감지
			s, err := c.detector.Detect(symbol, prices)
			if err != nil {
				log.Printf("시그널 감지 실패 (%s): %v", symbol, err)
				return nil
			}

			// 시그널 정보 로깅
			log.Printf("%s 시그널 감지 결과: %+v", symbol, s)

			if s != nil {
				if err := c.discord.SendSignal(s); err != nil {
					log.Printf("시그널 알림 전송 실패 (%s): %v", symbol, err)
				}

				if s.Type != signal.NoSignal {

					// 진입 가능 여부 확인
					result, err := c.checkEntryAvailable(ctx, s)
					if err != nil {
						if err := c.discord.SendError(err); err != nil {
							log.Printf("에러 알림 전송 실패: %v", err)
						}

					}

					if result {
						// 매매 실행
						if err := c.ExecuteSignalTrade(ctx, s); err != nil {
							c.discord.SendError(fmt.Errorf("매매 실행 실패: %v", err))
						} else {
							log.Printf("%s %s 포지션 진입 및 TP/SL 설정 완료",
								s.Symbol, s.Type.String())
						}
					}
				}
			}

			return nil
		})
		if err != nil {
			log.Printf("%s 심볼 데이터 수집 실패: %v", symbol, err)
			continue
		}
	}

	return nil
}

// CalculatePosition은 코인의 특성과 최소 주문 단위를 고려하여 실제 포지션 크기와 수량을 계산합니다
// 단계별 계산:
// 1. 이론적 최대 포지션 = 가용잔고 × 레버리지
// 2. 이론적 최대 수량 = 이론적 최대 포지션 ÷ 코인 가격
// 3. 실제 수량 = 이론적 최대 수량을 최소 주문 단위로 내림
// 4. 실제 포지션 가치 = 실제 수량 × 코인 가격
// 5. 수수료 및 마진 고려해 최종 조정
func (c *Collector) CalculatePosition(
	balance float64, // 가용 잔고
	totalBalance float64, // 총 잔고 (usdtBalance.CrossWalletBalance)
	leverage int, // 레버리지
	coinPrice float64, // 코인 현재 가격
	stepSize float64, // 코인 최소 주문 단위
	maintMargin float64, // 유지증거금률
) PositionSizeResult {
	// 1. 사용 가능한 잔고에서 항상 50%만 사용
	maxAllocationPercent := 0.5
	allocatedBalance := totalBalance * maxAllocationPercent

	// 단, 가용 잔고보다 큰 금액은 사용할 수 없음
	safeBalance := math.Min(balance, allocatedBalance)

	// 2. 레버리지 적용 및 수수료 고려
	totalFeeRate := 0.002 // 0.2% (진입 + 청산 수수료 + 여유분)
	effectiveMargin := maintMargin + totalFeeRate

	// 안전하게 사용 가능한 최대 포지션 가치 계산
	maxSafePositionValue := (safeBalance * float64(leverage)) / (1 + effectiveMargin)

	// 3. 최대 안전 수량 계산
	maxSafeQuantity := maxSafePositionValue / coinPrice

	// 4. 최소 주문 단위로 수량 조정
	// stepSize가 0.001이면 소수점 3자리
	precision := 0
	temp := stepSize
	for temp < 1.0 {
		temp *= 10
		precision++
	}

	// 소수점 자릿수에 맞춰 내림 계산
	scale := math.Pow(10, float64(precision))
	steps := math.Floor(maxSafeQuantity / stepSize)
	adjustedQuantity := steps * stepSize

	// 소수점 자릿수 정밀도 보장
	adjustedQuantity = math.Floor(adjustedQuantity*scale) / scale

	// 5. 최종 포지션 가치 계산
	finalPositionValue := adjustedQuantity * coinPrice

	// 포지션 크기에 대한 추가 안전장치 (최소값과 최대값 제한)
	finalPositionValue = math.Min(finalPositionValue, maxSafePositionValue)

	// 소수점 2자리까지 내림 (USDT 기준)
	return PositionSizeResult{
		PositionValue: math.Floor(finalPositionValue*100) / 100,
		Quantity:      adjustedQuantity,
	}
}

// findBracket은 주어진 레버리지에 해당하는 브라켓을 찾습니다
func findBracket(brackets []LeverageBracket, leverage int) *LeverageBracket {
	// 레버리지가 높은 순으로 정렬되어 있으므로,
	// 설정된 레버리지보다 크거나 같은 첫 번째 브라켓을 찾습니다.
	for i := len(brackets) - 1; i >= 0; i-- {
		if brackets[i].InitialLeverage >= leverage {
			return &brackets[i]
		}
	}

	// 찾지 못한 경우 가장 낮은 레버리지 브라켓 반환
	if len(brackets) > 0 {
		return &brackets[0]
	}
	return nil
}

func (c *Collector) checkEntryAvailable(ctx context.Context, coinSignal *signal.Signal) (bool, error) {
	// result := EntryCheckResult{
	// 	Available: false,
	// }

	// 1. 현재 포지션 조회
	positions, err := c.client.GetPositions(ctx)
	if err != nil {
		if len(positions) == 0 {
			log.Printf("활성 포지션 없음: %s", coinSignal.Symbol)
		} else {
			return false, err
		}

	}

	// 기존 포지션이 있는지 확인
	for _, pos := range positions {
		if pos.Symbol == coinSignal.Symbol && pos.Quantity != 0 {
			return false, fmt.Errorf("이미 %s에 대한 포지션이 있습니다. 수량: %.8f, 방향: %s",
				pos.Symbol, pos.Quantity, pos.PositionSide)
		}
	}

	// 2. 열린 주문 확인
	openOrders, err := c.client.GetOpenOrders(ctx, coinSignal.Symbol)
	if err != nil {
		return false, fmt.Errorf("주문 조회 실패: %w", err)
	}

	// 기존 TP/SL 주문이 있는지 확인
	if len(openOrders) > 0 {
		// 기존 주문 취소
		log.Printf("기존 주문 %d개를 취소합니다.", len(openOrders))
		for _, order := range openOrders {
			if err := c.client.CancelOrder(ctx, coinSignal.Symbol, order.OrderID); err != nil {
				return false, fmt.Errorf("주문 취소 실패 (ID: %d): %v", order.OrderID, err)
			}
		}
	}
	return true, nil
}

// TODO: 단순 상향돌파만 체크하는게 아니라 MACD가 0 이상인지 이하인지 그거도 추세 판단하는데 사용되는걸 적용해야한다.
// ExecuteSignalTrade는 감지된 시그널에 따라 매매를 실행합니다
func (c *Collector) ExecuteSignalTrade(ctx context.Context, s *signal.Signal) error {
	if s.Type == signal.NoSignal {
		return nil // 시그널이 없으면 아무것도 하지 않음
	}

	//---------------------------------
	// 1. 잔고 조회
	//---------------------------------
	var balances map[string]Balance
	err := c.withRetry(ctx, "잔고 조회", func() error {
		var err error
		balances, err = c.client.GetBalance(ctx)
		return err
	})
	if err != nil {
		return fmt.Errorf("잔고 조회 실패: %w", err)
	}

	//---------------------------------
	// 2. USDT 잔고 확인
	//---------------------------------
	usdtBalance, exists := balances["USDT"]
	if !exists || usdtBalance.Available <= 0 {
		return fmt.Errorf("USDT 잔고가 부족합니다")
	}

	//---------------------------------
	// 3. 현재 가격 조회 (최근 캔들 사용)
	//---------------------------------
	var candles []CandleData
	err = c.withRetry(ctx, "현재 가격 조회", func() error {
		candles, err = c.client.GetKlines(ctx, s.Symbol, "1m", 1)
		return err
	})
	if err != nil {
		return fmt.Errorf("가격 정보 조회 실패: %w", err)
	}
	if len(candles) == 0 {
		return fmt.Errorf("캔들 데이터를 가져오지 못했습니다")
	}
	currentPrice := candles[0].Close

	//---------------------------------
	// 4. 심볼 정보 조회
	//---------------------------------
	var symbolInfo *SymbolInfo
	err = c.withRetry(ctx, "현재 가격 조회", func() error {
		symbolInfo, err = c.client.GetSymbolInfo(ctx, s.Symbol)
		return err
	})
	if err != nil {
		return fmt.Errorf("심볼 정보 조회 실패: %w", err)
	}

	//---------------------------------
	// 5. HEDGE 모드 설정
	//---------------------------------
	err = c.withRetry(ctx, "HEDGE 모드 설정", func() error {
		err = c.client.SetPositionMode(ctx, true)
		return err
	})
	if err != nil {
		return fmt.Errorf("HEDGE 모드 설정 실패: %w", err)
	}

	//---------------------------------
	// 6. 레버리지 설정
	//---------------------------------
	leverage := c.config.Trading.Leverage
	err = c.withRetry(ctx, "레버리지 설정", func() error {
		err = c.client.SetLeverage(ctx, s.Symbol, leverage)
		return err
	})
	if err != nil {
		return fmt.Errorf("레버리지 설정 실패: %w", err)
	}

	//---------------------------------
	// 7. 매수 수량 계산 (잔고의 90% 사용)
	//---------------------------------
	// 레버리지 브라켓 정보 조회
	var brackets []SymbolBrackets
	err = c.withRetry(ctx, "매수 수량 계산", func() error {
		brackets, err = c.client.GetLeverageBrackets(ctx, s.Symbol)
		return err
	})
	if err != nil {
		return fmt.Errorf("레버리지 브라켓 조회 실패: %w", err)
	}

	// 해당 심볼의 브라켓 정보 찾기
	var symbolBracket *SymbolBrackets
	for _, b := range brackets {
		if b.Symbol == s.Symbol {
			symbolBracket = &b
			break
		}
	}

	if symbolBracket == nil || len(symbolBracket.Brackets) == 0 {
		return fmt.Errorf("레버리지 브라켓 정보가 없습니다")
	}

	// 설정된 레버리지에 맞는 브라켓 찾기
	bracket := findBracket(symbolBracket.Brackets, leverage)
	if bracket == nil {
		return fmt.Errorf("적절한 레버리지 브라켓을 찾을 수 없습니다")
	}

	// 포지션 크기 계산
	positionResult := c.CalculatePosition(
		usdtBalance.Available,
		usdtBalance.CrossWalletBalance,
		leverage,
		currentPrice,
		symbolInfo.StepSize,
		bracket.MaintMarginRatio,
	)

	// 최소 주문 가치 체크
	if positionResult.PositionValue < symbolInfo.MinNotional {
		return fmt.Errorf("포지션 크기가 최소 주문 가치(%.2f USDT)보다 작습니다", symbolInfo.MinNotional)
	}

	//---------------------------------
	// 8. 주문 수량 정밀도 조정
	//---------------------------------
	adjustedQuantity := AdjustQuantity(
		positionResult.Quantity,
		symbolInfo.StepSize,
		symbolInfo.QuantityPrecision,
	)

	//---------------------------------
	// 9. 진입 주문 생성
	//---------------------------------
	orderSide := Buy
	positionSide := Long
	if s.Type == signal.Short {
		orderSide = Sell
		positionSide = Short
	}

	entryOrder := OrderRequest{
		Symbol:       s.Symbol,
		Side:         orderSide,
		PositionSide: positionSide,
		Type:         Market,
		Quantity:     adjustedQuantity,
	}

	//---------------------------------
	// 10. 진입 주문 실행
	//---------------------------------
	var orderResponse *OrderResponse
	err = c.withRetry(ctx, "진입 주문 실행", func() error {
		orderResponse, err = c.client.PlaceOrder(ctx, entryOrder)
		return err
	})
	if err != nil {
		return fmt.Errorf("주문 실행 실패: %w", err)
	}

	//---------------------------------
	// 11. 성공 메시지 출력 및 로깅
	//---------------------------------
	log.Printf("매수 주문 성공: %s, 수량: %.8f, 주문 ID: %d",
		s.Symbol, adjustedQuantity, orderResponse.OrderID)

	//---------------------------------
	// 12. 포지션 확인 및 TP/SL 설정
	//---------------------------------
	maxRetries := 5
	retryInterval := 1 * time.Second
	var position *PositionInfo

	// 목표 포지션 사이드 문자열로 변환
	targetPositionSide := "LONG"
	if s.Type == signal.Short {
		targetPositionSide = "SHORT"
	}

	for i := 0; i < maxRetries; i++ {
		var positions []PositionInfo
		err = c.withRetry(ctx, "포지션 조회", func() error {
			positions, err = c.client.GetPositions(ctx)
			return err
		})
		if err != nil {
			log.Printf("포지션 조회 실패 (시도 %d/%d): %v", i+1, maxRetries, err)
			time.Sleep(retryInterval)
			continue
		}

		for _, pos := range positions {
			// 포지션 사이드 문자열 비교
			if pos.Symbol == s.Symbol && pos.PositionSide == targetPositionSide {
				// Long은 수량이 양수, Short은 음수이기 때문에 조건 분기
				positionValid := false
				if targetPositionSide == "LONG" && pos.Quantity > 0 {
					positionValid = true
				} else if targetPositionSide == "SHORT" && pos.Quantity < 0 {
					positionValid = true
				}

				if positionValid {
					position = &pos
					// log.Printf("포지션 확인: %s %s, 수량: %.8f, 진입가: %.2f",
					// 	pos.Symbol, pos.PositionSide, math.Abs(pos.Quantity), pos.EntryPrice)
					break
				}
			}
		}

		if position != nil {
			break
		}
		time.Sleep(retryInterval)
		retryInterval *= 2 // 지수 백오프
	}

	if position == nil {
		return fmt.Errorf("최대 재시도 횟수 초과: 포지션을 찾을 수 없음")
	}

	//---------------------------------
	// 13. TP/SL 값 설정
	//---------------------------------
	// 종료 주문을 위한 반대 방향 계산

	actualEntryPrice := position.EntryPrice
	actualQuantity := math.Abs(position.Quantity)

	var stopLoss, takeProfit float64
	if s.Type == signal.Long {
		slDistance := s.Price - s.StopLoss
		tpDistance := s.TakeProfit - s.Price
		stopLoss = actualEntryPrice - slDistance
		takeProfit = actualEntryPrice + tpDistance
	} else {
		slDistance := s.StopLoss - s.Price
		tpDistance := s.Price - s.TakeProfit
		stopLoss = actualEntryPrice + slDistance
		takeProfit = actualEntryPrice - tpDistance
	}

	// 가격 정밀도에 맞게 조정
	// symbolInfo.TickSize와 symbolInfo.PricePrecision 사용
	adjustStopLoss := AdjustPrice(stopLoss, symbolInfo.TickSize, symbolInfo.PricePrecision)
	adjustTakeProfit := AdjustPrice(takeProfit, symbolInfo.TickSize, symbolInfo.PricePrecision)

	// TP/SL 설정 알림
	if err := c.discord.SendInfo(fmt.Sprintf(
		"TP/SL 설정 중: %s\n진입가: %.2f\n수량: %.8f\n손절가: %.2f (-1%%)\n목표가: %.2f (+1%%)",
		s.Symbol, actualEntryPrice, actualQuantity, adjustStopLoss, adjustTakeProfit)); err != nil {
		log.Printf("TP/SL 설정 알림 전송 실패: %v", err)
	}

	//---------------------------------
	// 14. TP/SL 주문 생성
	//---------------------------------
	oppositeSide := Sell
	if s.Type == signal.Short {
		oppositeSide = Buy
	}
	// 손절 주문 생성
	slOrder := OrderRequest{
		Symbol:       s.Symbol,
		Side:         oppositeSide,
		PositionSide: positionSide,
		Type:         StopMarket,
		Quantity:     actualQuantity,
		StopPrice:    adjustStopLoss,
	}
	// 손절 주문 실행
	var slResponse *OrderResponse
	err = c.withRetry(ctx, "손절 주문 실행", func() error {
		slResponse, err = c.client.PlaceOrder(ctx, slOrder)
		return err
	})
	if err != nil {
		log.Printf("손절(SL) 주문 실패: %v", err)
		return fmt.Errorf("손절(SL) 주문 실패: %w", err)
	}

	// 익절 주문 생성
	tpOrder := OrderRequest{
		Symbol:       s.Symbol,
		Side:         oppositeSide,
		PositionSide: positionSide,
		Type:         TakeProfitMarket,
		Quantity:     actualQuantity,
		StopPrice:    adjustTakeProfit,
	}
	// 익절 주문 실행
	var tpResponse *OrderResponse
	err = c.withRetry(ctx, "익절 주문 실행", func() error {
		tpResponse, err = c.client.PlaceOrder(ctx, tpOrder)
		return err
	})
	if err != nil {
		log.Printf("익절(TP) 주문 실패: %v", err)
		return fmt.Errorf("익절(TP) 주문 실패: %w", err)
	}

	//---------------------------------
	// 15. TP/SL 설정 완료 알림
	//---------------------------------
	if err := c.discord.SendInfo(fmt.Sprintf("✅ TP/SL 설정 완료: %s\n익절(TP) 주문 성공: ID=%d 심볼=%s, 가격=%.2f, 수량=%.8f\n손절(SL) 주문 생성: ID=%d 심볼=%s, 가격=%.2f, 수량=%.8f", s.Symbol, tpResponse.OrderID, tpOrder.Symbol, tpOrder.StopPrice, tpOrder.Quantity, slResponse.OrderID, slOrder.Symbol, slOrder.StopPrice, slOrder.Quantity)); err != nil {
		log.Printf("TP/SL 설정 알림 전송 실패: %v", err)
	}

	//---------------------------------
	// 16. 거래 정보 생성 및 전송
	//---------------------------------
	tradeInfo := notification.TradeInfo{
		Symbol:        orderResponse.Symbol,
		PositionType:  targetPositionSide,
		PositionValue: positionResult.PositionValue,
		Quantity:      orderResponse.ExecutedQuantity,
		EntryPrice:    orderResponse.AvgPrice,
		StopLoss:      adjustStopLoss,
		TakeProfit:    adjustTakeProfit,
		Balance:       usdtBalance.Available - positionResult.PositionValue,
		Leverage:      leverage,
	}

	if err := c.discord.SendTradeInfo(tradeInfo); err != nil {
		log.Printf("거래 정보 알림 전송 실패: %v", err)
	}

	return nil
}

// AdjustQuantity는 바이낸스 최소 단위(stepSize)에 맞게 수량을 조정합니다
func AdjustQuantity(quantity float64, stepSize float64, precision int) float64 {
	if stepSize == 0 {
		return quantity // stepSize가 0이면 조정 불필요
	}

	// stepSize로 나누어 떨어지도록 조정
	steps := math.Floor(quantity / stepSize)
	adjustedQuantity := steps * stepSize

	// 정밀도에 맞게 반올림
	scale := math.Pow(10, float64(precision))
	return math.Floor(adjustedQuantity*scale) / scale
}

// getIntervalString은 수집 간격을 바이낸스 API 형식의 문자열로 변환합니다
func (c *Collector) getIntervalString() string {
	switch c.config.App.FetchInterval {
	case 1 * time.Minute:
		return "1m"
	case 3 * time.Minute:
		return "3m"
	case 5 * time.Minute:
		return "5m"
	case 15 * time.Minute:
		return "15m"
	case 30 * time.Minute:
		return "30m"
	case 1 * time.Hour:
		return "1h"
	case 2 * time.Hour:
		return "2h"
	case 4 * time.Hour:
		return "4h"
	case 6 * time.Hour:
		return "6h"
	case 8 * time.Hour:
		return "8h"
	case 12 * time.Hour:
		return "12h"
	case 24 * time.Hour:
		return "1d"
	default:
		return "15m" // 기본값
	}
}

// withRetry는 재시도 로직을 구현한 래퍼 함수입니다
func (c *Collector) withRetry(ctx context.Context, operation string, fn func() error) error {
	var lastErr error
	delay := c.retry.BaseDelay

	for attempt := 0; attempt <= c.retry.MaxRetries; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if err := fn(); err != nil {
				lastErr = err
				if attempt == c.retry.MaxRetries {
					// 마지막 시도에서 실패하면 Discord로 에러 알림 전송
					errMsg := fmt.Errorf("%s 실패 (최대 재시도 횟수 초과): %v", operation, err)
					if c.discord != nil {
						if notifyErr := c.discord.SendError(errMsg); notifyErr != nil {
							log.Printf("Discord 에러 알림 전송 실패: %v", notifyErr)
						}
					}
					return fmt.Errorf("최대 재시도 횟수 초과: %w", lastErr)
				}

				log.Printf("%s 실패 (attempt %d/%d): %v",
					operation, attempt+1, c.retry.MaxRetries, err)

				// 다음 재시도 전 대기
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(delay):
					// 대기 시간을 증가시키되, 최대 대기 시간을 넘지 않도록 함
					delay = time.Duration(float64(delay) * c.retry.Factor)
					if delay > c.retry.MaxDelay {
						delay = c.retry.MaxDelay
					}
				}
				continue
			}
			return nil
		}
	}
	return lastErr
}

// AdjustPrice는 가격 정밀도 설정 함수
func AdjustPrice(price float64, tickSize float64, precision int) float64 {
	if tickSize == 0 {
		return price // tickSize가 0이면 조정 불필요
	}

	// tickSize로 나누어 떨어지도록 조정
	ticks := math.Floor(price / tickSize)
	adjustedPrice := ticks * tickSize

	// 정밀도에 맞게 반올림
	scale := math.Pow(10, float64(precision))
	return math.Floor(adjustedPrice*scale) / scale
}

```
## internal/market/types.go
```go
package market

import "fmt"

// APIError는 바이낸스 API 에러를 표현합니다
type APIError struct {
	Code    int    `json:"code"`
	Message string `json:"msg"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("바이낸스 API 에러(코드: %d): %s", e.Code, e.Message)
}

// 에러 코드 상수 정의
const (
	ErrPositionModeNoChange = -4059 // 포지션 모드 변경 불필요 에러
)

// CandleData는 캔들 데이터를 표현합니다
type CandleData struct {
	OpenTime            int64   `json:"openTime"`
	Open                float64 `json:"open,string"`
	High                float64 `json:"high,string"`
	Low                 float64 `json:"low,string"`
	Close               float64 `json:"close,string"`
	Volume              float64 `json:"volume,string"`
	CloseTime           int64   `json:"closeTime"`
	QuoteVolume         float64 `json:"quoteVolume,string"`
	NumberOfTrades      int     `json:"numberOfTrades"`
	TakerBuyBaseVolume  float64 `json:"takerBuyBaseVolume,string"`
	TakerBuyQuoteVolume float64 `json:"takerBuyQuoteVolume,string"`
}

// Balance는 계정 잔고 정보를 표현합니다
type Balance struct {
	Asset              string  `json:"asset"`
	Available          float64 `json:"available"`
	Locked             float64 `json:"locked"`
	CrossWalletBalance float64 `json:"crossWalletBalance"`
}

// OrderSide는 주문 방향을 정의합니다
type OrderSide string

// PositionSide는 포지션 방향을 정의합니다
type PositionSide string

// OrderType은 주문 유형을 정의합니다
type OrderType string

const (
	Buy  OrderSide = "BUY"
	Sell OrderSide = "SELL"

	Long  PositionSide = "LONG"
	Short PositionSide = "SHORT"

	Market  OrderType = "MARKET"
	Limit   OrderType = "LIMIT"
	StopOCO OrderType = "STOP_OCO"

	StopMarket       OrderType = "STOP_MARKET"
	TakeProfitMarket OrderType = "TAKE_PROFIT_MARKET"
)

// OrderRequest는 주문 요청 정보를 표현합니다
type OrderRequest struct {
	Symbol        string
	Side          OrderSide
	PositionSide  PositionSide
	Type          OrderType
	Quantity      float64 // 코인 개수
	QuoteOrderQty float64 // USDT 가치 (추가됨)
	Price         float64 // 진입가격 (리밋 주문)
	StopPrice     float64 // 손절가격
	TakeProfit    float64 // 익절가격
}

// OrderResponse는 주문 응답을 표현합니다
type OrderResponse struct {
	OrderID          int64        `json:"orderId"`
	Symbol           string       `json:"symbol"`
	Status           string       `json:"status"`
	ClientOrderID    string       `json:"clientOrderId"`
	Price            float64      `json:"price,string"`
	AvgPrice         float64      `json:"avgPrice,string"`
	OrigQuantity     float64      `json:"origQty,string"`
	ExecutedQuantity float64      `json:"executedQty,string"`
	Side             string       `json:"side"`
	PositionSide     PositionSide `json:"positionSide"`
	Type             string       `json:"type"`
	CreateTime       int64        `json:"time"`
}

// SymbolVolume은 심볼의 거래량 정보를 표현합니다
type SymbolVolume struct {
	Symbol      string  `json:"symbol"`
	QuoteVolume float64 `json:"quoteVolume,string"`
}

type PositionInfo struct {
	Symbol       string  `json:"symbol"`
	PositionSide string  `json:"positionSide"`
	Quantity     float64 `json:"positionAmt,string"`
	EntryPrice   float64 `json:"entryPrice,string"`
}

// LeverageBracket은 레버리지 구간 정보를 나타냅니다
type LeverageBracket struct {
	Bracket          int     `json:"bracket"`          // 구간 번호
	InitialLeverage  int     `json:"initialLeverage"`  // 최대 레버리지
	MaintMarginRatio float64 `json:"maintMarginRatio"` // 유지증거금 비율
	Notional         float64 `json:"notional"`         // 명목가치 상한
}

// SymbolBrackets는 심볼별 레버리지 구간 정보를 나타냅니다
type SymbolBrackets struct {
	Symbol   string            `json:"symbol"`
	Brackets []LeverageBracket `json:"brackets"`
}

// SymbolInfo는 심볼의 거래 정보를 나타냅니다
type SymbolInfo struct {
	Symbol            string  // 심볼 이름 (예: BTCUSDT)
	StepSize          float64 // 수량 최소 단위 (예: 0.001 BTC)
	TickSize          float64 // 가격 최소 단위 (예: 0.01 USDT)
	MinNotional       float64 // 최소 주문 가치 (예: 10 USDT)
	PricePrecision    int     // 가격 소수점 자릿수
	QuantityPrecision int     // 수량 소수점 자릿수
}

// PositionSizeResult는 포지션 계산 결과를 담는 구조체입니다
type PositionSizeResult struct {
	PositionValue float64 // 포지션 크기 (USDT)
	Quantity      float64 // 구매 수량 (코인)
}

// EntryCheckResult는 진입 가능 여부 확인 결과를 담는 구조체입니다
type EntryCheckResult struct {
	Available     bool    // 진입 가능 여부
	Reason        string  // 불가능한 경우 이유
	PositionValue float64 // 포지션 크기 (USDT)
	Quantity      float64 // 구매/판매 수량 (코인)
}

// OrderInfo는 주문 정보를 표현합니다
type OrderInfo struct {
	OrderID          int64   `json:"orderId"`
	Symbol           string  `json:"symbol"`
	Status           string  `json:"status"`
	ClientOrderID    string  `json:"clientOrderId"`
	Price            float64 `json:"price,string"`
	OrigQuantity     float64 `json:"origQty,string"`
	ExecutedQuantity float64 `json:"executedQty,string"`
	Type             string  `json:"type"`
	Side             string  `json:"side"`
	PositionSide     string  `json:"positionSide"`
	StopPrice        float64 `json:"stopPrice,string"`
}

```
## internal/notification/discord/client.go
```go
package discord

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client는 Discord 웹훅 클라이언트입니다
type Client struct {
	signalWebhook string // 시그널 알림용 웹훅
	tradeWebhook  string // 거래 실행 알림용 웹훅
	errorWebhook  string // 에러 알림용 웹훅
	infoWebhook   string // 정보 알림용 웹훅
	client        *http.Client
}

// ClientOption은 Discord 클라이언트 옵션을 정의합니다
type ClientOption func(*Client)

// WithTimeout은 HTTP 클라이언트의 타임아웃을 설정합니다
func WithTimeout(timeout time.Duration) ClientOption {
	return func(c *Client) {
		c.client.Timeout = timeout
	}
}

// NewClient는 새로운 Discord 클라이언트를 생성합니다
func NewClient(signalWebhook, tradeWebhook, errorWebhook, infoWebhook string, opts ...ClientOption) *Client {
	c := &Client{
		signalWebhook: signalWebhook,
		tradeWebhook:  tradeWebhook,
		errorWebhook:  errorWebhook,
		infoWebhook:   infoWebhook,
		client:        &http.Client{Timeout: 10 * time.Second},
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// sendToWebhook은 지정된 웹훅으로 메시지를 전송합니다
func (c *Client) sendToWebhook(webhookURL string, message WebhookMessage) error {
	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("메시지 마샬링 실패: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, webhookURL, bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("요청 생성 실패: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("요청 전송 실패: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("웹훅 전송 실패: status=%d", resp.StatusCode)
	}

	return nil
}

```
## internal/notification/discord/embed.go
```go
package discord

import (
	"time"
)

// WebhookMessage는 Discord 웹훅 메시지를 정의합니다
type WebhookMessage struct {
	Content string  `json:"content,omitempty"`
	Embeds  []Embed `json:"embeds,omitempty"`
}

// Embed는 Discord 메시지 임베드를 정의합니다
type Embed struct {
	Title       string       `json:"title,omitempty"`
	Description string       `json:"description,omitempty"`
	Color       int          `json:"color,omitempty"`
	Fields      []EmbedField `json:"fields,omitempty"`
	Footer      *EmbedFooter `json:"footer,omitempty"`
	Timestamp   string       `json:"timestamp,omitempty"`
}

// EmbedField는 임베드 필드를 정의합니다
type EmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

// EmbedFooter는 임베드 푸터를 정의합니다
type EmbedFooter struct {
	Text string `json:"text"`
}

// NewEmbed는 새로운 임베드를 생성합니다
func NewEmbed() *Embed {
	return &Embed{}
}

// SetTitle은 임베드 제목을 설정합니다
func (e *Embed) SetTitle(title string) *Embed {
	e.Title = title
	return e
}

// SetDescription은 임베드 설명을 설정합니다
func (e *Embed) SetDescription(desc string) *Embed {
	e.Description = desc
	return e
}

// SetColor는 임베드 색상을 설정합니다
func (e *Embed) SetColor(color int) *Embed {
	e.Color = color
	return e
}

// AddField는 임베드에 필드를 추가합니다
func (e *Embed) AddField(name, value string, inline bool) *Embed {
	e.Fields = append(e.Fields, EmbedField{
		Name:   name,
		Value:  value,
		Inline: inline,
	})
	return e
}

// SetFooter는 임베드 푸터를 설정합니다
func (e *Embed) SetFooter(text string) *Embed {
	e.Footer = &EmbedFooter{Text: text}
	return e
}

// SetTimestamp는 임베드 타임스탬프를 설정합니다
func (e *Embed) SetTimestamp(t time.Time) *Embed {
	e.Timestamp = t.Format(time.RFC3339)
	return e
}

```
## internal/notification/discord/signal.go
```go
package discord

import (
	"fmt"

	"github.com/assist-by/phoenix/internal/analysis/signal"
	"github.com/assist-by/phoenix/internal/notification"
)

// SendSignal은 시그널 알림을 Discord로 전송합니다
func (c *Client) SendSignal(s *signal.Signal) error {
	var title, emoji string
	var color int

	switch s.Type {
	case signal.Long:
		emoji = "🚀"
		title = "LONG"
		color = notification.ColorSuccess
	case signal.Short:
		emoji = "🔻"
		title = "SHORT"
		color = notification.ColorError
	case signal.PendingLong:
		emoji = "⏳"
		title = "PENDING LONG"
		color = notification.ColorWarning
	case signal.PendingShort:
		emoji = "⏳"
		title = "PENDING SHORT"
		color = notification.ColorWarning
	default:
		emoji = "⚠️"
		title = "NO SIGNAL"
		color = notification.ColorInfo
	}

	// 시그널 조건 상태 표시
	longConditions := fmt.Sprintf(`%s EMA200 (가격이 EMA 위)
%s MACD (시그널 상향돌파)
%s SAR (SAR이 가격 아래)`,
		getCheckMark(s.Conditions.EMALong),
		getCheckMark(s.Conditions.MACDLong),
		getCheckMark(s.Conditions.SARLong))

	shortConditions := fmt.Sprintf(`%s EMA200 (가격이 EMA 아래)
		%s MACD (시그널 하향돌파)
		%s SAR (SAR이 가격 위)`,
		getCheckMark(s.Conditions.EMAShort),
		getCheckMark(s.Conditions.MACDShort),
		getCheckMark(s.Conditions.SARShort))

	// 기술적 지표 값
	technicalValues := fmt.Sprintf("```\n[EMA200]: %.5f\n[MACD Line]: %.5f\n[Signal Line]: %.5f\n[Histogram]: %.5f\n[SAR]: %.5f```",
		s.Conditions.EMAValue,
		s.Conditions.MACDValue,
		s.Conditions.SignalValue,
		s.Conditions.MACDValue-s.Conditions.SignalValue,
		s.Conditions.SARValue)

	embed := NewEmbed().
		SetTitle(fmt.Sprintf("%s %s %s/USDT", emoji, title, s.Symbol)).
		SetColor(color)

	if s.Type != signal.NoSignal {
		// 손익률 계산 및 표시
		var slPct, tpPct float64
		switch s.Type {
		case signal.Long:
			// Long: 실제 수치 그대로 표시
			slPct = (s.StopLoss - s.Price) / s.Price * 100
			tpPct = (s.TakeProfit - s.Price) / s.Price * 100
		case signal.Short:
			// Short: 부호 반대로 표시
			slPct = (s.Price - s.StopLoss) / s.Price * 100
			tpPct = (s.Price - s.TakeProfit) / s.Price * 100
		}

		embed.SetDescription(fmt.Sprintf(`**시간**: %s
 **현재가**: $%.2f
 **손절가**: $%.2f (%.2f%%)
 **목표가**: $%.2f (%.2f%%)`,
			s.Timestamp.Format("2006-01-02 15:04:05 KST"),
			s.Price,
			s.StopLoss,
			slPct,
			s.TakeProfit,
			tpPct,
		))
	} else if s.Type == signal.PendingLong || s.Type == signal.PendingShort {
		// 대기 상태 정보 표시
		var waitingFor string
		if s.Type == signal.PendingLong {
			waitingFor = "SAR가 캔들 아래로 이동 대기 중"
		} else {
			waitingFor = "SAR가 캔들 위로 이동 대기 중"
		}

		embed.SetDescription(fmt.Sprintf(`**시간**: %s
**현재가**: $%.2f
**대기 상태**: %s
**조건**: MACD 크로스 발생, SAR 위치 부적절`,
			s.Timestamp.Format("2006-01-02 15:04:05 KST"),
			s.Price,
			waitingFor,
		))
	} else {
		embed.SetDescription(fmt.Sprintf(`**시간**: %s
 **현재가**: $%.2f`,
			s.Timestamp.Format("2006-01-02 15:04:05 KST"),
			s.Price,
		))
	}

	embed.AddField("LONG 조건", longConditions, true)
	embed.AddField("SHORT 조건", shortConditions, true)
	embed.AddField("기술적 지표", technicalValues, false)

	return c.sendToWebhook(c.signalWebhook, WebhookMessage{
		Embeds: []Embed{*embed},
	})
}

func getCheckMark(condition bool) string {
	if condition {
		return "✅"
	}
	return "❌"
}

```
## internal/notification/discord/webhook.go
```go
package discord

import (
	"fmt"
	"strings"
	"time"

	"github.com/assist-by/phoenix/internal/notification"
)

// SendSignal은 시그널 알림을 전송합니다
// func (c *Client) SendSignal(signal notification.Signal) error {
// 	embed := NewEmbed().
// 		SetTitle(fmt.Sprintf("트레이딩 시그널: %s", signal.Symbol)).
// 		SetDescription(fmt.Sprintf("**타입**: %s\n**가격**: $%.2f\n**이유**: %s",
// 			signal.Type, signal.Price, signal.Reason)).
// 		SetColor(getColorForSignal(signal.Type)).
// 		SetFooter("Assist by Trading Bot 🤖").
// 		SetTimestamp(signal.Timestamp)

// 	msg := WebhookMessage{
// 		Embeds: []Embed{*embed},
// 	}

// 	return c.sendToWebhook(c.signalWebhook, msg)
// }

// SendError는 에러 알림을 전송합니다
func (c *Client) SendError(err error) error {
	embed := NewEmbed().
		SetTitle("에러 발생").
		SetDescription(fmt.Sprintf("```%v```", err)).
		SetColor(notification.ColorError).
		SetFooter("Assist by Trading Bot 🤖").
		SetTimestamp(time.Now())

	msg := WebhookMessage{
		Embeds: []Embed{*embed},
	}

	return c.sendToWebhook(c.errorWebhook, msg)
}

// SendInfo는 일반 정보 알림을 전송합니다
func (c *Client) SendInfo(message string) error {
	embed := NewEmbed().
		SetDescription(message).
		SetColor(notification.ColorInfo).
		SetFooter("Assist by Trading Bot 🤖").
		SetTimestamp(time.Now())

	msg := WebhookMessage{
		Embeds: []Embed{*embed},
	}

	return c.sendToWebhook(c.infoWebhook, msg)
}

// SendTradeInfo는 거래 실행 정보를 전송합니다
func (c *Client) SendTradeInfo(info notification.TradeInfo) error {
	embed := NewEmbed().
		SetTitle(fmt.Sprintf("거래 실행: %s", info.Symbol)).
		SetDescription(fmt.Sprintf(
			"**포지션**: %s\n**수량**: %.8f %s\n**포지션 크기**: %.2f USDT\n**레버리지**: %dx\n**진입가**: $%.2f\n**손절가**: $%.2f\n**목표가**: $%.2f\n**현재 잔고**: %.2f USDT",
			info.PositionType,
			info.Quantity,
			strings.TrimSuffix(info.Symbol, "USDT"), // BTCUSDT에서 BTC만 추출
			info.PositionValue,
			info.Leverage,
			info.EntryPrice,
			info.StopLoss,
			info.TakeProfit,
			info.Balance,
		)).
		SetColor(notification.GetColorForPosition(info.PositionType)).
		SetFooter("Assist by Trading Bot 🤖").
		SetTimestamp(time.Now())

	msg := WebhookMessage{
		Embeds: []Embed{*embed},
	}

	return c.sendToWebhook(c.tradeWebhook, msg)
}

```
## internal/notification/types.go
```go
package notification

import "time"

// SignalType은 트레이딩 시그널 종류를 정의합니다
type SignalType string

const (
	SignalLong         SignalType = "LONG"
	SignalShort        SignalType = "SHORT"
	SignalClose        SignalType = "CLOSE"
	SignalPendingLong  SignalType = "PENDINGLONG"  // 롱 대기 상태
	SignalPendingShort SignalType = "PENDINGSHORT" // 숏 대기 상태
	ColorSuccess                  = 0x00FF00
	ColorError                    = 0xFF0000
	ColorInfo                     = 0x0000FF
	ColorWarning                  = 0xFFA500 // 대기 상태를 위한 주황색 추가
)

// Signal은 트레이딩 시그널 정보를 담는 구조체입니다
type Signal struct {
	Type      SignalType
	Symbol    string
	Price     float64
	Timestamp time.Time
	Reason    string
}

// Notifier는 알림 전송 인터페이스를 정의합니다
type Notifier interface {
	// SendSignal은 트레이딩 시그널 알림을 전송합니다
	SendSignal(signal Signal) error

	// SendError는 에러 알림을 전송합니다
	SendError(err error) error

	// SendInfo는 일반 정보 알림을 전송합니다
	SendInfo(message string) error
}

// TradeInfo는 거래 실행 정보를 정의합니다
type TradeInfo struct {
	Symbol        string  // 심볼 (예: BTCUSDT)
	PositionType  string  // "LONG" or "SHORT"
	PositionValue float64 // 포지션 크기 (USDT)
	Quantity      float64 // 구매/판매 수량 (코인)
	EntryPrice    float64 // 진입가
	StopLoss      float64 // 손절가
	TakeProfit    float64 // 익절가
	Balance       float64 // 현재 USDT 잔고
	Leverage      int     // 사용 레버리지
}

// getColorForPosition은 포지션 타입에 따른 색상을 반환합니다
func GetColorForPosition(positionType string) int {
	switch positionType {
	case "LONG":
		return ColorSuccess
	case "SHORT":
		return ColorError
	case "PENDINGLONG", "PENDINGSHORT":
		return ColorWarning
	default:
		return ColorInfo
	}
}

```
## internal/scheduler/scheduler.go
```go
package scheduler

import (
	"context"
	"log"
	"time"
)

// Task는 스케줄러가 실행할 작업을 정의하는 인터페이스입니다
type Task interface {
	Execute(ctx context.Context) error
}

// Scheduler는 정해진 시간에 작업을 실행하는 스케줄러입니다
type Scheduler struct {
	interval time.Duration
	task     Task
	stopCh   chan struct{}
}

// NewScheduler는 새로운 스케줄러를 생성합니다
func NewScheduler(interval time.Duration, task Task) *Scheduler {
	return &Scheduler{
		interval: interval,
		task:     task,
		stopCh:   make(chan struct{}),
	}
}

// Start는 스케줄러를 시작합니다
// internal/scheduler/scheduler.go

func (s *Scheduler) Start(ctx context.Context) error {
	// 다음 실행 시간 계산
	now := time.Now()
	nextRun := now.Truncate(s.interval).Add(s.interval)
	waitDuration := nextRun.Sub(now)

	log.Printf("다음 실행까지 %v 대기 (다음 실행: %s)",
		waitDuration.Round(time.Second),
		nextRun.Format("15:04:05"))

	timer := time.NewTimer(waitDuration)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-s.stopCh:
			return nil

		case <-timer.C:
			// 작업 실행
			if err := s.task.Execute(ctx); err != nil {
				log.Printf("작업 실행 실패: %v", err)
				// 에러가 발생해도 계속 실행
			}

			// 다음 실행 시간 계산
			now := time.Now()
			nextRun = now.Truncate(s.interval).Add(s.interval)
			waitDuration = nextRun.Sub(now)

			log.Printf("다음 실행까지 %v 대기 (다음 실행: %s)",
				waitDuration.Round(time.Second),
				nextRun.Format("15:04:05"))

			// 타이머 리셋
			timer.Reset(waitDuration)
		}
	}
}

// Stop은 스케줄러를 중지합니다
func (s *Scheduler) Stop() {
	close(s.stopCh)
}

```
