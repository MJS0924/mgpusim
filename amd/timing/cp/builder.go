package cp

import (
	"fmt"

	"github.com/sarchlab/akita/v4/analysis"
	"github.com/sarchlab/akita/v4/monitoring"
	"github.com/sarchlab/akita/v4/sim"
	"github.com/sarchlab/akita/v4/tracing"
	"github.com/sarchlab/mgpusim/v4/amd/driver"
	"github.com/sarchlab/mgpusim/v4/amd/protocol"
	"github.com/sarchlab/mgpusim/v4/amd/timing/cp/internal/dispatching"
	"github.com/sarchlab/mgpusim/v4/amd/timing/cp/internal/resource"
)

// Builder can build Command Processors
type Builder struct {
	deviceID            uint32
	freq                sim.Freq
	engine              sim.Engine
	visTracer           tracing.Tracer
	monitor             *monitoring.Monitor
	perfAnalyzer        *analysis.PerfAnalyzer
	driver              *driver.Driver
	numDispatchers      int
	pageMigrationPolicy uint64
}

// MakeBuilder creates a new builder with default configuration values.
func MakeBuilder() Builder {
	b := Builder{
		freq:           1 * sim.GHz,
		numDispatchers: 8,
	}
	return b
}

// WithVisTracer enables tracing for visualization on the command processor and
// the dispatchers.
func (b Builder) WithDeviceID(deviceID uint32) Builder {
	b.deviceID = deviceID
	return b
}

// WithVisTracer enables tracing for visualization on the command processor and
// the dispatchers.
func (b Builder) WithVisTracer(tracer tracing.Tracer) Builder {
	b.visTracer = tracer
	return b
}

// WithEngine sets the even-driven simulation engine to use.
func (b Builder) WithEngine(engine sim.Engine) Builder {
	b.engine = engine
	return b
}

// WithFreq sets the frequency that the Command Processor works at.
func (b Builder) WithFreq(freq sim.Freq) Builder {
	b.freq = freq
	return b
}

// WithMonitor sets the monitor used to show progress bars.
func (b Builder) WithMonitor(monitor *monitoring.Monitor) Builder {
	b.monitor = monitor
	return b
}

// WithPerfAnalyzer sets the buffer analyzer used to analyze the
// command processor's buffers.
func (b Builder) WithPerfAnalyzer(
	analyzer *analysis.PerfAnalyzer,
) Builder {
	b.perfAnalyzer = analyzer
	return b
}

// WithPerfAnalyzer sets the buffer analyzer used to analyze the
// command processor's buffers.
func (b Builder) WithDriver(
	driver *driver.Driver,
) Builder {
	b.driver = driver
	return b
}

func (b Builder) WithPageMigrationPolicy(policy uint64) Builder {
	b.pageMigrationPolicy = policy
	return b
}

// Build builds a new Command Processor
func (b Builder) Build(name string) *CommandProcessor {
	cp := new(CommandProcessor)
	cp.deviceID = b.deviceID
	cp.TickingComponent = sim.NewTickingComponent(name, b.engine, b.freq, cp)
	cp.pageMigrationPolicy = b.pageMigrationPolicy

	cp.Driver = b.driver.GetPortByName("GPU")

	b.createPorts(cp, name)

	cp.bottomKernelLaunchReqIDToTopReqMap =
		make(map[string]*protocol.LaunchKernelReq)
	cp.bottomMemCopyH2DReqIDToTopReqMap =
		make(map[string]*protocol.MemCopyH2DReq)
	cp.bottomMemCopyD2HReqIDToTopReqMap =
		make(map[string]*protocol.MemCopyD2HReq)

	tracing.CollectTrace(cp, b.visTracer)

	b.buildDispatchers(cp)

	if b.perfAnalyzer != nil {
		b.perfAnalyzer.RegisterComponent(cp)
	}

	cp.returnValue = append(cp.returnValue, false)
	cp.returnValue = append(cp.returnValue, false)
	cp.returnValue = append(cp.returnValue, false)
	cp.returnValue = append(cp.returnValue, false)

	return cp
}

func (Builder) createPorts(cp *CommandProcessor, name string) {
	cp.ToDriver = sim.NewPort(cp, 4096, 4096, name+".ToDriver")
	cp.ToDMA = sim.NewPort(cp, 4096, 4096, name+".ToDispatcher")
	cp.ToCUs = sim.NewPort(cp, 4096, 4096, name+".ToCUs")
	cp.ToROBs = sim.NewPort(cp, 4096, 4096, name+".ToROBs")
	cp.ToTLBs = sim.NewPort(cp, 4096, 4096, name+".ToTLBs")
	cp.ToRDMA = sim.NewPort(cp, 4096, 4096, name+".ToRDMA")
	cp.ToPMC = sim.NewPort(cp, 4096, 4096, name+".ToPMC")
	cp.ToAddressTranslators = sim.NewPort(cp, 4096, 4096,
		name+".ToAddressTranslators")
	cp.ToCohDir = sim.NewPort(cp, 4096, 4096, name+".ToCohDir")
	cp.ToCaches = sim.NewPort(cp, 4096, 4096, name+".ToCaches")
	cp.ToGMMU = sim.NewPort(cp, 4096, 4096, name+".ToGMMU")
}

func (b *Builder) buildDispatchers(cp *CommandProcessor) {
	cuResourcePool := resource.NewCUResourcePool()
	builder := dispatching.MakeBuilder().
		WithCP(cp).
		WithAlg("round-robin").
		WithCUResourcePool(cuResourcePool).
		WithDispatchingPort(cp.ToCUs).
		WithRespondingPort(cp.ToDriver).
		WithMonitor(b.monitor)

	for i := 0; i < b.numDispatchers; i++ {
		disp := builder.Build(fmt.Sprintf("%s.Dispatcher%d", cp.Name(), i))

		if b.visTracer != nil {
			tracing.CollectTrace(disp, b.visTracer)
		}

		cp.Dispatchers = append(cp.Dispatchers, disp)
	}
}
