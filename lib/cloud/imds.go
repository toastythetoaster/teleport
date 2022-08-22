/*
Copyright 2022 Gravitational, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cloud

import "context"

// InstanceMetadata is an interface for fetching information from a cloud
// service's instance metadata.
type InstanceMetadata interface {
	// IsAvailable checks if instance metadata is available.
	IsAvailable(ctx context.Context) bool
	// GetTags gets all of the instance's tags.
	GetTags(ctx context.Context) (map[string]string, error)
}
