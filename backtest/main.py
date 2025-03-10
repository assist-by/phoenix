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