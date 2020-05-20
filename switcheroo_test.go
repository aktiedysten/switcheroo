package switcheroo

/* NOTE this only contains superficial unit-testing; see test/ for a full
 * real-world test using iptables */

import "testing"
import "sort"

func TestSort(t *testing.T) {
	rules := iptablesRules{
		iptablesRule{
			Num: 42,
		},
		iptablesRule{
			Num: 1,
		},
		iptablesRule{
			Num: 420,
		},
		iptablesRule{
			Num: 240,
		},
	}

	sort.Sort(iptablesRulesByNumDescending(rules))

	{
		expect := func(index, expectedNum int) {
			actualNum := rules[index].Num
			if actualNum != expectedNum {
				t.Errorf("epxected rules[%d].Num = %d; got %d", index, expectedNum, actualNum)
			}
		}
		expect(0, 420)
		expect(1, 240)
		expect(2, 42)
		expect(3, 1)
	}
}
