package docker

import (
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/volume/mounts"
	"github.com/purpleidea/mgmt/lang/funcs/simple"
	"github.com/purpleidea/mgmt/lang/types"
)

func init() {
	simple.ModuleRegister("docker", "parse_volume", &types.FuncValue{
		T: types.NewType("func(str) struct{Type str; Source str; Target str; ReadOnly bool; Consistency str; BindOptions struct{Propagation str; NonRecursive bool}; VolumeOptions struct{NoCopy bool; Labels map{str: str}; DriverConfig struct{Name str; Options map{str: str}}}; TmpfsOptions struct{SizeBytes int; Mode int}}"),
		V: func(values []types.Value) (types.Value, error) {
			input := values[0].Str()

			mp, err := mounts.NewParser(mounts.OSLinux).ParseMountRaw(input, "")
			if err != nil {
				v, _ := types.ValueOfGolang(mount.Mount{})
				return v, err
			}
			return types.ValueOfGolang(mp.Spec)
		},
	})
}
