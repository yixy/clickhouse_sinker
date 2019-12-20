/*Copyright [2019] housepower

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

   http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"flag"
	"github.com/housepower/clickhouse_sinker/creator"
	"github.com/housepower/clickhouse_sinker/prom"
	"github.com/housepower/clickhouse_sinker/statistics"
	"github.com/housepower/clickhouse_sinker/task"
	_ "github.com/kshvakov/clickhouse"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"net/http"

	"github.com/wswz/go_commons/app"
	"github.com/wswz/go_commons/log"
)

var (
	config    = flag.String("conf", "", "config dir")
	http_addr = flag.String("http-addr", "0.0.0.0:2112", "http interface")
)

func init() {
	flag.Parse()

	prometheus.MustRegister(prometheus.NewBuildInfoCollector())
	prometheus.MustRegister(prom.ClickhouseReconnectTotal)
	prometheus.MustRegister(prom.ClickhouseEventsSuccess)
	prometheus.MustRegister(prom.ClickhouseEventsErrors)
	prometheus.MustRegister(prom.ClickhouseEventsTotal)
	prometheus.MustRegister(prom.KafkaConsumerErrors)

}

func main() {

	var cfg creator.Config
	var runner *Sinker

	app.Run("clickhouse_sinker", func() error {
		cfg = *creator.InitConfig(*config)
		runner = NewSinker(cfg)
		return runner.Init()
	}, func() error {
		go func() {
			log.Info("Run http server", *http_addr)
			http.Handle("/metrics", promhttp.Handler())
			log.Error(http.ListenAndServe(*http_addr, nil))
		}()

		runner.Run()
		return nil
	}, func() error {
		runner.Close()
		return nil
	})
}

// Sinker object maintains number of task for each partition
type Sinker struct {
	tasks   []*task.TaskService
	pusher  *statistics.Pusher
	config  creator.Config
	stopped chan struct{}
}

// NewSinker get an instance of sinker with the task list
func NewSinker(config creator.Config) *Sinker {
	s := &Sinker{config: config, stopped: make(chan struct{})}
	return s
}

// Init initializes the list of tasks
func (s *Sinker) Init() error {
	if s.config.Statistics.Enable {
		s.pusher = statistics.NewPusher(s.config.Statistics.PushGateWayAddrs,
			s.config.Statistics.PushInterval)
		err := s.pusher.Init()
		if err != nil {
			return err
		}
	}
	s.tasks = s.config.GenTasks()
	for _, t := range s.tasks {
		if err := t.Init(); err != nil {
			return err
		}
	}
	return nil
}

// Run rull all tasks in different go routines
func (s *Sinker) Run() {
	if s.pusher != nil {
		s.pusher.Run()
	}
	for i := range s.tasks {
		go s.tasks[i].Run()
	}
	<-s.stopped
}

// Close shoutdown tasks
func (s *Sinker) Close() {
	for i := range s.tasks {
		s.tasks[i].Stop()
	}

	if s.pusher != nil {
		s.pusher.Stop()
	}
	close(s.stopped)
}
