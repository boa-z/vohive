package traffic

import (
	"context"
	"errors"

	"github.com/zanescope/quectel-qmi-go/pkg/qmi"
)

type qmiWDSPacketStatisticsReader interface {
	WDSGetPacketStatistics(ctx context.Context, mask uint32) (*qmi.PacketStatistics, error)
}

func readQMIWDSTrafficCounters(ctx context.Context, reader qmiWDSPacketStatisticsReader) (trafficCounters, error) {
	stats, err := reader.WDSGetPacketStatistics(ctx, qmiWDSTrafficStatsMask)
	if err != nil {
		return trafficCounters{}, err
	}
	if stats == nil {
		return trafficCounters{}, errors.New("empty qmi wds packet statistics")
	}
	return trafficCounters{RXBytes: stats.RxBytesOK, TXBytes: stats.TxBytesOK}, nil
}
