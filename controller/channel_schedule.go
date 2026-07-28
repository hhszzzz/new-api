package controller

import (
	"time"

	"github.com/QuantumNous/new-api/model"
)

func populateChannelScheduleStates(channels []*model.Channel) {
	now := time.Now()
	for _, channel := range channels {
		channel.PopulateScheduleStateAt(now)
	}
}
