package vm

import (
	"errors"
	"fmt"

	"github.com/inoio/agents-sandbox/internal/sandbox/state"
)

func persistEnvSecrets(k state.Key, envState state.EnvState, secretState state.SecretState) error {
	st, err := state.ReadState(k)
	if err != nil {
		if errors.Is(err, state.ErrStateNotFound) {
			st = new(state.HomeState)
		} else {
			return fmt.Errorf("read state for persistence: %w", err)
		}
	}
	st.EnvState = envState
	st.SecretState = secretState
	return state.WriteState(k, *st)
}

func persistNetworkState(k state.Key, networkState state.NetworkState) error {
	st, err := state.ReadState(k)
	if err != nil {
		if errors.Is(err, state.ErrStateNotFound) {
			st = new(state.HomeState)
		} else {
			return fmt.Errorf("read state for network persistence: %w", err)
		}
	}
	st.NetworkState = networkState
	return state.WriteState(k, *st)
}

func persistMountState(k state.Key, mountState state.MountState) error {
	st, err := state.ReadState(k)
	if err != nil {
		if errors.Is(err, state.ErrStateNotFound) {
			st = new(state.HomeState)
		} else {
			return fmt.Errorf("read state for mount persistence: %w", err)
		}
	}
	st.MountState = mountState
	return state.WriteState(k, *st)
}
