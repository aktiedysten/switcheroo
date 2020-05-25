package switcheroo

import "net"
import "os"
import "os/exec"
import "fmt"
import "strconv"
import "strings"
import "regexp"
import "syscall"
import "log"
import "sort"

func isAddressAlreadyInUseError(err error) bool {
	if operr, ok := err.(*net.OpError); ok {
		if scerr, ok := operr.Err.(*os.SyscallError); ok {
			if scerr.Err == syscall.EADDRINUSE {
				return true
			}
		}
	}
	return false
}

type iptablesRule struct {
	Chain string
	Num   int
	Port  int
	PID   int
}

func (r *iptablesRule) String() string {
	return fmt.Sprintf("[chain=%s;num=%d;port=%d;pid=%d]", r.Chain, r.Num, r.Port, r.PID)
}

type iptablesRules []iptablesRule

func (rs iptablesRules) String() string {
	var xs []string
	for _, r := range rs {
		xs = append(xs, r.String())
	}
	return strings.Join(xs, ",")
}

type iptablesRulesByNumDescending iptablesRules

func (rs iptablesRulesByNumDescending) Len() int           { return len(rs) }
func (rs iptablesRulesByNumDescending) Swap(i, j int)      { rs[i], rs[j] = rs[j], rs[i] }
func (rs iptablesRulesByNumDescending) Less(i, j int) bool { return !(rs[i].Num < rs[j].Num) }

/* You can change all these after NewSwitcheroo() and before switcheroo.Begin()
 * */
type Switcheroo struct {
	// Namespacing is required if you plan on running multiple
	// switcheroo-powered processes on the same computer
	Namespace string

	// The port we accept traffic on, seen from the outside
	IncomingPort int

	// Dynamic port range min/max
	PortMin int
	PortMax int

	// func mimicing exec.Command(...), but you can override it (e.g. we
	// support both `iptables` and `sudo iptables` via this interface)
	IptablesCmd func(args ...string) *exec.Cmd

	// If set, we log what happens. Can be nil.
	Logger *log.Logger

	// See SetFlags()
	Flags int
}

func (s *Switcheroo) deleteIptablesRule(rule *iptablesRule) error {
	return s.IptablesCmd("-t", "nat", "-D", rule.Chain, strconv.Itoa(rule.Num)).Run()
}

func (s *Switcheroo) getChains() []string {
	chains := []string{}
	if (s.Flags & ENABLE_NETWORK) != 0 {
		chains = append(chains, "PREROUTING")
	}
	if (s.Flags & ENABLE_LOCALHOST) != 0 {
		chains = append(chains, "OUTPUT")
	}
	return chains
}

func inStringArray(x string, xs []string) bool {
	for _, s := range xs {
		if s == x {
			return true
		}
	}
	return false
}

func (s *Switcheroo) getSwitcherooles() (iptablesRules, error) {
	rules := iptablesRules{}

	for _, chain := range s.getChains() {
		out, err := s.IptablesCmd("-L", chain, "-n", "-t", "nat", "--line-numbers").CombinedOutput()
		if err != nil {
			return nil, err
		}

		re := regexp.MustCompile(`^(\d+).*SWITCHEROO:ns=` + s.Namespace + `:port=(\d+):pid=(\d+)`)
		for _, line := range strings.Split(string(out), "\n") {
			mo := re.FindStringSubmatch(line)
			if mo == nil || len(mo) != 4 {
				continue
			}

			ruleNum, err := strconv.Atoi(mo[1])
			if err != nil {
				return nil, fmt.Errorf("invalid rulenum %s", mo[1])
			}

			port, err := strconv.Atoi(mo[2])
			if err != nil {
				return nil, fmt.Errorf("invalid port %s", mo[2])
			}

			pid, err := strconv.Atoi(mo[3])
			if err != nil {
				return nil, fmt.Errorf("invalid PID %s", mo[3])
			}

			rules = append(rules, iptablesRule{
				Chain: chain,
				Num:   ruleNum,
				Port:  port,
				PID:   pid,
			})
		}
	}

	/* This sort makes it safe to remove rules in the order returned here.
	 * The problem is that rule numbers are re-ordered when deleted, so
	 * it's only safe to delete rules in descending order (Alternatively we
	 * could generate a new rule list for every delete, but that's a waste
	 * of resources) */
	sort.Sort(iptablesRulesByNumDescending(rules))

	return rules, nil
}

func (s *Switcheroo) mkLogf(stage string) func(string, ...interface{}) {
	if s.Logger == nil {
		/* no logger given; return a no-op logf() */
		return func(format string, args ...interface{}) {
		}
	} else {
		prefix := fmt.Sprintf("[switcheroo/ns=%s/pid=%d/stage=%s] ", s.Namespace, os.Getpid(), stage)
		return func(format string, args ...interface{}) {
			s.Logger.Printf("%s%s", prefix, fmt.Sprintf(format, args...))
		}
	}
}

/* Begins the switcheroo process; returns a net.Listener you should start your
 * server on, and when you're ready to serve traffic, call the returned
 * function, which finalizes the process */
func (s *Switcheroo) Begin() (net.Listener, func() error, error) {
	logf := s.mkLogf("start")

	var listener net.Listener
	var newPort int
	{
		rules, err := s.getSwitcherooles()
		if err != nil {
			return nil, nil, err
		}

		if len(rules) > 0 {
			logf("Existing rules found: %s (n=%d)", rules.String(), len(rules))
		}

		portsInUse := map[int]bool{}
		var maxPortInUse int
		for _, rule := range rules {
			port := rule.Port
			portsInUse[port] = true
			if port > maxPortInUse {
				maxPortInUse = port
			}
		}

		/* Attempt to allocate a new port */
		n := s.PortMax - s.PortMin + 1
		var tryPort, attempts int
		if maxPortInUse > 0 {
			tryPort = maxPortInUse + 1
		} else {
			tryPort = s.PortMin
		}
		for i := 0; i < n; i++ {
			attempts++
			tryPort = ((tryPort - s.PortMin) % (s.PortMax - s.PortMin + 1)) + s.PortMin
			if portsInUse[tryPort] {
				continue
			}
			listener, err = net.Listen("tcp", ":"+strconv.Itoa(tryPort))
			if isAddressAlreadyInUseError(err) {
				tryPort++
				continue
			} else if err != nil {
				return nil, nil, err
			} else {
				newPort = tryPort
				break
			}
		}
		if newPort == 0 {
			return nil, nil, fmt.Errorf("found no available port in range [%d:%d]", s.PortMin, s.PortMax)
		} else {
			logf("Port %d was allocated (attempts: %d)", newPort, attempts)
		}
	}

	/* now the caller must start its server on `listener` (and optionally
	 * do a self-check); when up and running it must call the
	 * closure/function we return here; it completes the process by
	 * installing a new iptables rule (traffic is not routed to us before
	 * that happens), and cleaning up in old iptables rules and processes
	 * */

	return listener, func() error {
		logf := s.mkLogf("finalize")

		/* Set up new iptables rule that redirects traffic on
		 * `incomingPort` to `newPort`. The new rule is prepended, so
		 * it actually supercedes/outranks previous rules for
		 * `incomingPort` */
		newPortStr := strconv.Itoa(newPort)
		chains := s.getChains()
		for _, chain := range chains {
			err := s.IptablesCmd(
				"-t", "nat",
				"-I", chain,
				"-p", "tcp",
				"--dport", strconv.Itoa(s.IncomingPort),
				"-j", "REDIRECT", "--to-ports", newPortStr,
				"-m", "comment", "--comment", "SWITCHEROO:ns="+s.Namespace+":port="+newPortStr+":pid="+strconv.Itoa(os.Getpid()),
			).Run()
			if err != nil {
				return err
			}
		}

		logf("Installed iptables %s rule for routing :%d -> :%d", strings.Join(chains, "+"), s.IncomingPort, newPort)

		rules, err := s.getSwitcherooles()
		if err != nil {
			return err
		}

		/* Remove old iptables rules and processes. Don't stop on
		 * errors; the success of the previous Command() implies that
		 * traffic is now being forwarded to us (and maybe we've even
		 * confirmed it works); so at this point we simply want to
		 * clean up as well as we can */
		var nRulesDeleted int
		killSet := map[int]bool{}
		for _, rule := range rules {
			if rule.Port == newPort {
				// don't delete the rule just inserted
				continue
			}
			err = s.deleteIptablesRule(&rule)
			if err != nil {
				logf("ERROR %s", err.Error())
			} else {
				nRulesDeleted++
			}

			killSet[rule.PID] = true
		}

		var nProcessesKilled int
		for pid := range killSet {
			err = syscall.Kill(pid, syscall.SIGTERM)
			if err != nil {
				logf("WARNING; kill -TERM %d; %s", pid, err.Error())
			} else {
				nProcessesKilled++
			}
		}

		logf("rules deleted: %d / processes killed: %d / DONE", nRulesDeleted, nProcessesKilled)

		return nil
	}, nil
}

/* Remove all switcheroo iptables rules */
func (s *Switcheroo) Cleanup() error {
	logf := s.mkLogf("cleanup")

	rules, err := s.getSwitcherooles()
	if err != nil {
		return err
	}
	for _, rule := range rules {
		logf("deleting rule %s", rule.String())
		err = s.deleteIptablesRule(&rule)
		if err != nil {
			return err
		}
	}
	return nil
}

/* use with SetFlags() */
const (
	/* enables network and/or localhost routing; iptables is a bit
	 * "low-level weird" in this regard, because loopback/localhost routing
	 * and network/hardware-level routing occurs in different "chains"; so
	 * a localhost rule won't affect "traffic from the outside", and vice
	 * versa  */
	ENABLE_NETWORK   = 1 << 0
	ENABLE_LOCALHOST = 1 << 1
)

/* see constants above */
func (s *Switcheroo) SetFlags(flags int) {
	s.Flags = flags
}

func NewSwitcherooWithIptablesCmd(namespace string, incomingPort int, logger *log.Logger, iptablesCmd func(args ...string) *exec.Cmd) *Switcheroo {
	const namespaceMaxLength = 180
	if len(namespace) > namespaceMaxLength {
		panic(fmt.Sprintf("namespace is too long (%d chars); the limit is set to %d chars to avoid exceeding the 256 byte limit for iptables comments", len(namespace), namespaceMaxLength))
	}

	return &Switcheroo{
		Namespace:    namespace,
		IncomingPort: incomingPort,
		IptablesCmd:  iptablesCmd,
		PortMin:      40400,
		PortMax:      40499,
		Logger:       logger,
		Flags:        ENABLE_NETWORK,
	}
}

/* returns Switcheroo that uses iptables as-is (so you must be root) */
func NewSwitcheroo(namespace string, incomingPort int, logger *log.Logger) (*Switcheroo, error) {
	iptables, err := exec.LookPath("iptables")
	if err != nil {
		return nil, fmt.Errorf("failed to locate iptables binary: %s", err.Error())
	}
	iptablesCmd := func(args ...string) *exec.Cmd {
		return exec.Command(iptables, args...)
	}
	return NewSwitcherooWithIptablesCmd(namespace, incomingPort, logger, iptablesCmd), nil
}

/* returns Switcheroo that executes iptables using sudo (so your user must have
 * sudo-access to iptables) */
func NewSwitcherooWithSudoIptables(namespace string, incomingPort int, logger *log.Logger) (*Switcheroo, error) {
	sudo, err := exec.LookPath("sudo")
	if err != nil {
		return nil, fmt.Errorf("failed to locate sudo binary: %s", err.Error())
	}

	iptables, err := exec.LookPath("iptables")
	if err != nil {
		return nil, fmt.Errorf("failed to locate iptables binary: %s", err.Error())
	}

	iptablesCmd := func(args ...string) *exec.Cmd {
		return exec.Command(sudo, append([]string{iptables}, args...)...)
	}

	return NewSwitcherooWithIptablesCmd(namespace, incomingPort, logger, iptablesCmd), nil
}
