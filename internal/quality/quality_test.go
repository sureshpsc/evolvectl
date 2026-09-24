package quality

import "testing"

func TestParseGoTestAndDiff(t *testing.T) {
	beforeText := "" +
		"--- PASS: TestKeep (0.00s)\n" +
		"--- FAIL: TestOld (0.00s)\n" +
		"FAIL\n" +
		"FAIL\texample.com/pay/client\t0.01s\tcoverage: 70.0% of statements\n"

	samples, fails, passed := ParseGoTest(beforeText)
	if passed != 1 {
		t.Fatalf("passed %d", passed)
	}
	if len(fails) != 1 || fails[0].Name != "TestOld" || fails[0].Package != "example.com/pay/client" {
		t.Fatalf("fails %+v", fails)
	}
	if len(samples) != 1 || !samples[0].HasPercent || samples[0].Percent != 70 {
		t.Fatalf("samples %+v", samples)
	}

	afterText := "" +
		"--- PASS: TestKeep (0.00s)\n" +
		"--- PASS: TestOld (0.00s)\n" +
		"--- FAIL: TestNew (0.00s)\n" +
		"FAIL\n" +
		"FAIL\texample.com/pay/client\t0.02s\tcoverage: 66.0% of statements\n"
	_, afterFails, afterPassed := ParseGoTest(afterText)
	if afterPassed != 2 {
		t.Fatalf("after passed %d", afterPassed)
	}
	q := Add(nil, "before", samples, fails, 1)
	q = Add(q, "after", nil, afterFails, afterPassed)
	if len(q.NewlyFailed) != 1 || q.NewlyFailed[0].Name != "TestNew" {
		t.Fatalf("newly %+v", q.NewlyFailed)
	}
	if len(q.Fixed) != 1 || q.Fixed[0].Name != "TestOld" {
		t.Fatalf("fixed %+v", q.Fixed)
	}
	if len(q.StillFailing) != 0 {
		t.Fatalf("still %+v", q.StillFailing)
	}
}

func TestBuildFailureAndCoverTotal(t *testing.T) {
	_, fails, passed := ParseGoTest("FAIL\texample.com/pay\t[build failed]\n")
	if passed != 0 || len(fails) != 1 || fails[0].Name != "(build)" {
		t.Fatalf("fails %+v passed %d", fails, passed)
	}
	pct, ok := ParseCoverFunc("example.com/pay/client.go:10:\t\tFoo\t\t100.0%\ntotal:\t\t\t\t\t\t(statements)\t81.5%\n")
	if !ok || pct != 81.5 {
		t.Fatalf("pct %v %v", pct, ok)
	}
}

func TestPytestAndCoverageReport(t *testing.T) {
	text := "" +
		"app/test_models.py::test_dump PASSED\n" +
		"app/test_models.py::test_old FAILED\n" +
		"FAILED app/test_models.py::test_old - AssertionError\n"
	fails, passed := ParsePytest(text)
	if passed != 1 || len(fails) != 1 || fails[0].Name != "test_old" {
		t.Fatalf("passed %d fails %+v", passed, fails)
	}
	pct, ok := ParseCoverageTotal("Name    Stmts   Miss  Cover\nTOTAL     40      4    90%\n")
	if !ok || pct != 90 {
		t.Fatalf("pct %v %v", pct, ok)
	}
}
