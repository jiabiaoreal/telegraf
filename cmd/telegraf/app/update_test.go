package app

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDate_DateMarshal(t *testing.T) {
	type args struct {
		data []byte
	}
	tmp, _ := time.Parse(dateFormat, "2017-06-02 11:52")
	d := Date(tmp)
	tests := []struct {
		name    string
		d       *Date
		args    args
		wantErr bool
	}{
		{
			name: "test marsharl",
			d:    &d,
			args: args{
				data: []byte("2017-06-02 11:52"),
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if dat, err := json.Marshal(tt.d); (err != nil) != tt.wantErr || string(dat) != string(tt.args.data) {
				t.Errorf("Date.Marshal() error = %v, wantErr %v", err, tt.wantErr)
			}
			tmp := Date{}
			if err := json.Unmarshal(tt.args.data, &tmp); (err != nil) != tt.wantErr || !time.Time(tmp).Equal(time.Time(*tt.d)) {
				t.Errorf("Date.Marshal() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
