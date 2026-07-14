package service

import (
	"errors"
	"testing"
)

func TestPortForwardServiceConvertsInputAndView(t *testing.T) {
	app := testApp(t)
	view, err := app.CreatePortForward(PortForwardInput{
		Name: " Web ", ListenPort: 19090, Protocol: " TCP ", TargetHost: "10.0.0.2", TargetPort: 80,
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.ID == 0 || view.Name != "Web" || view.Protocol != "tcp" || view.TargetDisplay != "10.0.0.2:80" {
		t.Fatalf("view = %+v", view)
	}
	list, err := app.ListPortForwards()
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Rules) != 1 || list.Rules[0] != view {
		t.Fatalf("list = %+v, want %v", list.Rules, view)
	}
}

func TestPortForwardServiceMapsNotFound(t *testing.T) {
	app := testApp(t)
	if _, err := app.UpdatePortForward(99, PortForwardInput{ListenPort: 9000, Protocol: "tcp", TargetHost: "10.0.0.2", TargetPort: 80}); !errors.Is(err, ErrPortForwardNotFound) {
		t.Fatalf("UpdatePortForward error = %v, want %v", err, ErrPortForwardNotFound)
	}
}

func TestPortForwardDeleteAbsentIDIsIdempotentAndReconciles(t *testing.T) {
	app := testApp(t)
	net := &phase5Network{}
	dp := &characterizationDataplane{}
	app.Hub.SetNetworkRuntime(net)
	app.Hub.dpMu.Lock()
	app.Hub.liveDP = dp
	app.Hub.dpMu.Unlock()

	if err := app.DeletePortForward(99); err != nil {
		t.Fatal(err)
	}
	if len(dp.fullSyncs) != 1 {
		t.Fatalf("full sync calls = %d, want 1", len(dp.fullSyncs))
	}
}

func TestPortForwardServiceMapsOccupiedListenPort(t *testing.T) {
	app := testApp(t)
	if _, err := app.CreatePortForward(PortForwardInput{ListenPort: 19090, Protocol: "tcp", TargetHost: "10.0.0.2", TargetPort: 80}); err != nil {
		t.Fatal(err)
	}
	_, err := app.CreatePortForward(PortForwardInput{ListenPort: 19090, Protocol: "udp", TargetHost: "10.0.0.3", TargetPort: 80})
	var portErr *PortForwardListenPortError
	if !errors.As(err, &portErr) || portErr.Error() != "listen port 19090 is already in use" {
		t.Fatalf("occupied port error = %v, want typed error with preserved text", err)
	}
}
