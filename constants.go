package main

import "time"

const (
	numberOfCalculatingServers = 5                // number of servers that are active after server starts
	heartbeatDuration          = time.Second * 2  // (Cache.heartBeatDuration)
	checkInterval              = time.Second      // (Cache.interval)
	defaultTimerDuration       = time.Second * 10 // (Operation.Duration)
	addDuration                = time.Second * 10 // duration that adds after clicking "ADD TIME" on site
)
