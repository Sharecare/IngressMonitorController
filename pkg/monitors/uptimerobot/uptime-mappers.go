package uptimerobot

import (
	"strconv"

	"github.com/stakater/IngressMonitorController/pkg/models"
)

func UptimeMonitorMonitorToBaseMonitorMapper(uptimeMonitor UptimeMonitorMonitor) *models.Monitor {
	var m models.Monitor

	m.Name = uptimeMonitor.FriendlyName
	m.URL = uptimeMonitor.URL
	m.ID = strconv.Itoa(uptimeMonitor.ID)
	m.Interval = uptimeMonitor.Interval

	return &m
}

func UptimeMonitorMonitorsToBaseMonitorsMapper(uptimeMonitors []UptimeMonitorMonitor) []models.Monitor {
	var monitors []models.Monitor

	for index := 0; index < len(uptimeMonitors); index++ {
		monitors = append(monitors, *UptimeMonitorMonitorToBaseMonitorMapper(uptimeMonitors[index]))
	}

	return monitors
}
