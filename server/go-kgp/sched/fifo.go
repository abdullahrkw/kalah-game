// Connection Queue Handling
//
// Copyright (c) 2021, 2022, 2023  Philip Kaludercic
//
// This file is part of go-kgp.
//
// go-kgp is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License,
// version 3, as published by the Free Software Foundation.
//
// go-kgp is distributed in the hope that it will be useful, but
// WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU
// Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public
// License, version 3, along with go-kgp. If not, see
// <http://www.gnu.org/licenses/>

package sched

import (
	"log"
	"math/rand"
	"sync"
	"time"

	"go-kgp"
	"go-kgp/bot"
	"go-kgp/conf"
	"go-kgp/game"
)

// The intent is not to have a secure source of random values, but
// just to avoid a predictive shuffling of north/south positions.
func init() { rand.Seed(time.Now().UnixMicro()) }

type fifo struct {
	conf *conf.Conf
	add  chan kgp.Agent
	rem  chan kgp.Agent
	shut chan struct{}
	wait sync.WaitGroup
}

func (f *fifo) Start() {
	var (
		bots []kgp.Agent
		q    []kgp.Agent
		bi   int // bot index
		done bool
	)

	for _, d := range f.conf.BotTypes {
		bots = append(bots, bot.MakeMinMax(d))
	}

	for {
		select {
		case <-f.shut:
			// Stop accepting new connections
			done = true
			continue
		case a := <-f.add:
			if done {
				kgp.Debug.Println("Ignore", a)
				continue
			}
			kgp.Debug.Println("Schedule", a)
			if !bot.IsBot(a) {
				q = append(q, a)

				// The idea here is to give clients
				// the chance to meet up, instead of
				// directly pairing them up with a
				// bot.  The disadvantage is that
				// fewer games are played, but this
				// also reduces the server load.
				time.Sleep(5 * time.Second)
				kgp.Debug.Println("Waiting:", len(f.add))
				if len(f.add) > 0 {
					continue
				}
			}
		case a := <-f.rem:
			kgp.Debug.Println("Remove", a)
			for i := range q {
				if q[i] != a {
					continue
				}

				q[i] = q[len(q)-1]
				q = q[:len(q)-1]
			}
			continue
		}
		kgp.Debug.Print(q)

		// Remove all dead agents
		i := 0
		for _, a := range q {
			if a != nil && a.Alive() {
				q[i] = a
				i++
			}
		}
		q = q[:i]
		kgp.Debug.Print(q)

		// Select two agents, or two agents and a bot if only
		// one agent is available.
		var north, south kgp.Agent
		switch len(q) {
		case 0:
			continue
		case 1:
			south = q[0]
			q = nil

			// rotate through all bots
			bi = (bi + 1) % len(bots)
			north = bots[bi]
		default:
			south = q[0]
			north = q[1]
			q[0] = q[len(q)-1]
			q[1] = q[len(q)-2]
			q = q[:len(q)-2]

			// Prevent an agent from playing against
			// himself (note that this does not prevent
			// two separate agents with the same token to
			// challenge one another)
			if north == south {
				q = append(q, north)
				continue
			}

		}
		kgp.Debug.Println("Selected", north, south)

		// Ensure that we actually have two agents
		if north == nil || south == nil {
			panic("Illegal state")
		}

		// Start a game, but shuffle the order to avoid an
		// advantage for bots or non-bots.
		if rand.Intn(2) == 0 {
			north, south = south, north
		}

		f.wait.Add(1)
		go func(north, south kgp.Agent) {
			game.Play(&kgp.Game{
				Board: kgp.MakeBoard(
					f.conf.BoardSize,
					f.conf.BoardInit),
				South: north,
				North: south,
			}, f.conf)
			f.Schedule(south)
			f.Schedule(north)
			f.wait.Done()
		}(north, south)
	}
}

func (f *fifo) Shutdown() {
	log.Println("Waiting for ongoing games to finish.")
	f.shut <- struct{}{}
	f.wait.Wait()
}

func (f *fifo) Schedule(a kgp.Agent)   { f.add <- a }
func (f *fifo) Unschedule(a kgp.Agent) { f.rem <- a }
func (*fifo) String() string           { return "FIFO Scheduler" }

func MakeFIFO(config *conf.Conf) conf.GameManager {
	var man conf.GameManager = &fifo{
		add:  make(chan kgp.Agent, 16),
		rem:  make(chan kgp.Agent, 16),
		shut: make(chan struct{}),
		conf: config,
	}
	return man
}
