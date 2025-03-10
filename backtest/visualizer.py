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