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