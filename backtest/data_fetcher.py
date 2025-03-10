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