package crowd

import "testing"

func TestEvaluateRequiresBothAbsoluteAndBaselineThresholds(t *testing.T) {
	if got := Evaluate(2, 0); got.Anomalous || got.Threshold != 3 {
		t.Fatalf("small spike = %+v", got)
	}
	if got := Evaluate(3, 0); !got.Anomalous {
		t.Fatalf("first meaningful spike = %+v", got)
	}
	if got := Evaluate(4, 1); got.Anomalous || got.Threshold != 5 {
		t.Fatalf("baseline not respected = %+v", got)
	}
	if got := Evaluate(5, 1); !got.Anomalous {
		t.Fatalf("baseline spike = %+v", got)
	}
}
