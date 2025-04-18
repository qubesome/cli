// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.36.5
// 	protoc        v5.28.3
// source: pkg/inception/proto/host.proto

package proto

import (
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	reflect "reflect"
	sync "sync"
	unsafe "unsafe"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

type XdgOpenRequest struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Url           string                 `protobuf:"bytes,1,opt,name=url,proto3" json:"url,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *XdgOpenRequest) Reset() {
	*x = XdgOpenRequest{}
	mi := &file_pkg_inception_proto_host_proto_msgTypes[0]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *XdgOpenRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*XdgOpenRequest) ProtoMessage() {}

func (x *XdgOpenRequest) ProtoReflect() protoreflect.Message {
	mi := &file_pkg_inception_proto_host_proto_msgTypes[0]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use XdgOpenRequest.ProtoReflect.Descriptor instead.
func (*XdgOpenRequest) Descriptor() ([]byte, []int) {
	return file_pkg_inception_proto_host_proto_rawDescGZIP(), []int{0}
}

func (x *XdgOpenRequest) GetUrl() string {
	if x != nil {
		return x.Url
	}
	return ""
}

type XdgOpenReply struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *XdgOpenReply) Reset() {
	*x = XdgOpenReply{}
	mi := &file_pkg_inception_proto_host_proto_msgTypes[1]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *XdgOpenReply) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*XdgOpenReply) ProtoMessage() {}

func (x *XdgOpenReply) ProtoReflect() protoreflect.Message {
	mi := &file_pkg_inception_proto_host_proto_msgTypes[1]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use XdgOpenReply.ProtoReflect.Descriptor instead.
func (*XdgOpenReply) Descriptor() ([]byte, []int) {
	return file_pkg_inception_proto_host_proto_rawDescGZIP(), []int{1}
}

type RunWorkloadRequest struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Workload      string                 `protobuf:"bytes,1,opt,name=workload,proto3" json:"workload,omitempty"`
	Args          string                 `protobuf:"bytes,2,opt,name=args,proto3" json:"args,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *RunWorkloadRequest) Reset() {
	*x = RunWorkloadRequest{}
	mi := &file_pkg_inception_proto_host_proto_msgTypes[2]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *RunWorkloadRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*RunWorkloadRequest) ProtoMessage() {}

func (x *RunWorkloadRequest) ProtoReflect() protoreflect.Message {
	mi := &file_pkg_inception_proto_host_proto_msgTypes[2]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use RunWorkloadRequest.ProtoReflect.Descriptor instead.
func (*RunWorkloadRequest) Descriptor() ([]byte, []int) {
	return file_pkg_inception_proto_host_proto_rawDescGZIP(), []int{2}
}

func (x *RunWorkloadRequest) GetWorkload() string {
	if x != nil {
		return x.Workload
	}
	return ""
}

func (x *RunWorkloadRequest) GetArgs() string {
	if x != nil {
		return x.Args
	}
	return ""
}

type RunWorkloadReply struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *RunWorkloadReply) Reset() {
	*x = RunWorkloadReply{}
	mi := &file_pkg_inception_proto_host_proto_msgTypes[3]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *RunWorkloadReply) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*RunWorkloadReply) ProtoMessage() {}

func (x *RunWorkloadReply) ProtoReflect() protoreflect.Message {
	mi := &file_pkg_inception_proto_host_proto_msgTypes[3]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use RunWorkloadReply.ProtoReflect.Descriptor instead.
func (*RunWorkloadReply) Descriptor() ([]byte, []int) {
	return file_pkg_inception_proto_host_proto_rawDescGZIP(), []int{3}
}

type FlatpakRunWorkloadRequest struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	Workload      string                 `protobuf:"bytes,1,opt,name=workload,proto3" json:"workload,omitempty"`
	Args          string                 `protobuf:"bytes,2,opt,name=args,proto3" json:"args,omitempty"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *FlatpakRunWorkloadRequest) Reset() {
	*x = FlatpakRunWorkloadRequest{}
	mi := &file_pkg_inception_proto_host_proto_msgTypes[4]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *FlatpakRunWorkloadRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*FlatpakRunWorkloadRequest) ProtoMessage() {}

func (x *FlatpakRunWorkloadRequest) ProtoReflect() protoreflect.Message {
	mi := &file_pkg_inception_proto_host_proto_msgTypes[4]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use FlatpakRunWorkloadRequest.ProtoReflect.Descriptor instead.
func (*FlatpakRunWorkloadRequest) Descriptor() ([]byte, []int) {
	return file_pkg_inception_proto_host_proto_rawDescGZIP(), []int{4}
}

func (x *FlatpakRunWorkloadRequest) GetWorkload() string {
	if x != nil {
		return x.Workload
	}
	return ""
}

func (x *FlatpakRunWorkloadRequest) GetArgs() string {
	if x != nil {
		return x.Args
	}
	return ""
}

type FlatpakRunWorkloadReply struct {
	state         protoimpl.MessageState `protogen:"open.v1"`
	unknownFields protoimpl.UnknownFields
	sizeCache     protoimpl.SizeCache
}

func (x *FlatpakRunWorkloadReply) Reset() {
	*x = FlatpakRunWorkloadReply{}
	mi := &file_pkg_inception_proto_host_proto_msgTypes[5]
	ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
	ms.StoreMessageInfo(mi)
}

func (x *FlatpakRunWorkloadReply) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*FlatpakRunWorkloadReply) ProtoMessage() {}

func (x *FlatpakRunWorkloadReply) ProtoReflect() protoreflect.Message {
	mi := &file_pkg_inception_proto_host_proto_msgTypes[5]
	if x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use FlatpakRunWorkloadReply.ProtoReflect.Descriptor instead.
func (*FlatpakRunWorkloadReply) Descriptor() ([]byte, []int) {
	return file_pkg_inception_proto_host_proto_rawDescGZIP(), []int{5}
}

var File_pkg_inception_proto_host_proto protoreflect.FileDescriptor

var file_pkg_inception_proto_host_proto_rawDesc = string([]byte{
	0x0a, 0x1e, 0x70, 0x6b, 0x67, 0x2f, 0x69, 0x6e, 0x63, 0x65, 0x70, 0x74, 0x69, 0x6f, 0x6e, 0x2f,
	0x70, 0x72, 0x6f, 0x74, 0x6f, 0x2f, 0x68, 0x6f, 0x73, 0x74, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f,
	0x12, 0x08, 0x71, 0x75, 0x62, 0x65, 0x73, 0x6f, 0x6d, 0x65, 0x22, 0x22, 0x0a, 0x0e, 0x58, 0x64,
	0x67, 0x4f, 0x70, 0x65, 0x6e, 0x52, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x12, 0x10, 0x0a, 0x03,
	0x75, 0x72, 0x6c, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x03, 0x75, 0x72, 0x6c, 0x22, 0x0e,
	0x0a, 0x0c, 0x58, 0x64, 0x67, 0x4f, 0x70, 0x65, 0x6e, 0x52, 0x65, 0x70, 0x6c, 0x79, 0x22, 0x44,
	0x0a, 0x12, 0x52, 0x75, 0x6e, 0x57, 0x6f, 0x72, 0x6b, 0x6c, 0x6f, 0x61, 0x64, 0x52, 0x65, 0x71,
	0x75, 0x65, 0x73, 0x74, 0x12, 0x1a, 0x0a, 0x08, 0x77, 0x6f, 0x72, 0x6b, 0x6c, 0x6f, 0x61, 0x64,
	0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x08, 0x77, 0x6f, 0x72, 0x6b, 0x6c, 0x6f, 0x61, 0x64,
	0x12, 0x12, 0x0a, 0x04, 0x61, 0x72, 0x67, 0x73, 0x18, 0x02, 0x20, 0x01, 0x28, 0x09, 0x52, 0x04,
	0x61, 0x72, 0x67, 0x73, 0x22, 0x12, 0x0a, 0x10, 0x52, 0x75, 0x6e, 0x57, 0x6f, 0x72, 0x6b, 0x6c,
	0x6f, 0x61, 0x64, 0x52, 0x65, 0x70, 0x6c, 0x79, 0x22, 0x4b, 0x0a, 0x19, 0x46, 0x6c, 0x61, 0x74,
	0x70, 0x61, 0x6b, 0x52, 0x75, 0x6e, 0x57, 0x6f, 0x72, 0x6b, 0x6c, 0x6f, 0x61, 0x64, 0x52, 0x65,
	0x71, 0x75, 0x65, 0x73, 0x74, 0x12, 0x1a, 0x0a, 0x08, 0x77, 0x6f, 0x72, 0x6b, 0x6c, 0x6f, 0x61,
	0x64, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x08, 0x77, 0x6f, 0x72, 0x6b, 0x6c, 0x6f, 0x61,
	0x64, 0x12, 0x12, 0x0a, 0x04, 0x61, 0x72, 0x67, 0x73, 0x18, 0x02, 0x20, 0x01, 0x28, 0x09, 0x52,
	0x04, 0x61, 0x72, 0x67, 0x73, 0x22, 0x19, 0x0a, 0x17, 0x46, 0x6c, 0x61, 0x74, 0x70, 0x61, 0x6b,
	0x52, 0x75, 0x6e, 0x57, 0x6f, 0x72, 0x6b, 0x6c, 0x6f, 0x61, 0x64, 0x52, 0x65, 0x70, 0x6c, 0x79,
	0x32, 0xf8, 0x01, 0x0a, 0x0c, 0x51, 0x75, 0x62, 0x65, 0x73, 0x6f, 0x6d, 0x65, 0x48, 0x6f, 0x73,
	0x74, 0x12, 0x3d, 0x0a, 0x07, 0x58, 0x64, 0x67, 0x4f, 0x70, 0x65, 0x6e, 0x12, 0x18, 0x2e, 0x71,
	0x75, 0x62, 0x65, 0x73, 0x6f, 0x6d, 0x65, 0x2e, 0x58, 0x64, 0x67, 0x4f, 0x70, 0x65, 0x6e, 0x52,
	0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x1a, 0x16, 0x2e, 0x71, 0x75, 0x62, 0x65, 0x73, 0x6f, 0x6d,
	0x65, 0x2e, 0x58, 0x64, 0x67, 0x4f, 0x70, 0x65, 0x6e, 0x52, 0x65, 0x70, 0x6c, 0x79, 0x22, 0x00,
	0x12, 0x49, 0x0a, 0x0b, 0x52, 0x75, 0x6e, 0x57, 0x6f, 0x72, 0x6b, 0x6c, 0x6f, 0x61, 0x64, 0x12,
	0x1c, 0x2e, 0x71, 0x75, 0x62, 0x65, 0x73, 0x6f, 0x6d, 0x65, 0x2e, 0x52, 0x75, 0x6e, 0x57, 0x6f,
	0x72, 0x6b, 0x6c, 0x6f, 0x61, 0x64, 0x52, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x1a, 0x1a, 0x2e,
	0x71, 0x75, 0x62, 0x65, 0x73, 0x6f, 0x6d, 0x65, 0x2e, 0x52, 0x75, 0x6e, 0x57, 0x6f, 0x72, 0x6b,
	0x6c, 0x6f, 0x61, 0x64, 0x52, 0x65, 0x70, 0x6c, 0x79, 0x22, 0x00, 0x12, 0x5e, 0x0a, 0x12, 0x46,
	0x6c, 0x61, 0x74, 0x70, 0x61, 0x6b, 0x52, 0x75, 0x6e, 0x57, 0x6f, 0x72, 0x6b, 0x6c, 0x6f, 0x61,
	0x64, 0x12, 0x23, 0x2e, 0x71, 0x75, 0x62, 0x65, 0x73, 0x6f, 0x6d, 0x65, 0x2e, 0x46, 0x6c, 0x61,
	0x74, 0x70, 0x61, 0x6b, 0x52, 0x75, 0x6e, 0x57, 0x6f, 0x72, 0x6b, 0x6c, 0x6f, 0x61, 0x64, 0x52,
	0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x1a, 0x21, 0x2e, 0x71, 0x75, 0x62, 0x65, 0x73, 0x6f, 0x6d,
	0x65, 0x2e, 0x46, 0x6c, 0x61, 0x74, 0x70, 0x61, 0x6b, 0x52, 0x75, 0x6e, 0x57, 0x6f, 0x72, 0x6b,
	0x6c, 0x6f, 0x61, 0x64, 0x52, 0x65, 0x70, 0x6c, 0x79, 0x22, 0x00, 0x42, 0x2d, 0x5a, 0x2b, 0x67,
	0x69, 0x74, 0x68, 0x75, 0x62, 0x2e, 0x63, 0x6f, 0x6d, 0x2f, 0x71, 0x75, 0x62, 0x65, 0x73, 0x6f,
	0x6d, 0x65, 0x2f, 0x63, 0x6c, 0x69, 0x2f, 0x70, 0x6b, 0x67, 0x2f, 0x69, 0x6e, 0x63, 0x65, 0x70,
	0x74, 0x69, 0x6f, 0x6e, 0x2f, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x06, 0x70, 0x72, 0x6f, 0x74,
	0x6f, 0x33,
})

var (
	file_pkg_inception_proto_host_proto_rawDescOnce sync.Once
	file_pkg_inception_proto_host_proto_rawDescData []byte
)

func file_pkg_inception_proto_host_proto_rawDescGZIP() []byte {
	file_pkg_inception_proto_host_proto_rawDescOnce.Do(func() {
		file_pkg_inception_proto_host_proto_rawDescData = protoimpl.X.CompressGZIP(unsafe.Slice(unsafe.StringData(file_pkg_inception_proto_host_proto_rawDesc), len(file_pkg_inception_proto_host_proto_rawDesc)))
	})
	return file_pkg_inception_proto_host_proto_rawDescData
}

var file_pkg_inception_proto_host_proto_msgTypes = make([]protoimpl.MessageInfo, 6)
var file_pkg_inception_proto_host_proto_goTypes = []any{
	(*XdgOpenRequest)(nil),            // 0: qubesome.XdgOpenRequest
	(*XdgOpenReply)(nil),              // 1: qubesome.XdgOpenReply
	(*RunWorkloadRequest)(nil),        // 2: qubesome.RunWorkloadRequest
	(*RunWorkloadReply)(nil),          // 3: qubesome.RunWorkloadReply
	(*FlatpakRunWorkloadRequest)(nil), // 4: qubesome.FlatpakRunWorkloadRequest
	(*FlatpakRunWorkloadReply)(nil),   // 5: qubesome.FlatpakRunWorkloadReply
}
var file_pkg_inception_proto_host_proto_depIdxs = []int32{
	0, // 0: qubesome.QubesomeHost.XdgOpen:input_type -> qubesome.XdgOpenRequest
	2, // 1: qubesome.QubesomeHost.RunWorkload:input_type -> qubesome.RunWorkloadRequest
	4, // 2: qubesome.QubesomeHost.FlatpakRunWorkload:input_type -> qubesome.FlatpakRunWorkloadRequest
	1, // 3: qubesome.QubesomeHost.XdgOpen:output_type -> qubesome.XdgOpenReply
	3, // 4: qubesome.QubesomeHost.RunWorkload:output_type -> qubesome.RunWorkloadReply
	5, // 5: qubesome.QubesomeHost.FlatpakRunWorkload:output_type -> qubesome.FlatpakRunWorkloadReply
	3, // [3:6] is the sub-list for method output_type
	0, // [0:3] is the sub-list for method input_type
	0, // [0:0] is the sub-list for extension type_name
	0, // [0:0] is the sub-list for extension extendee
	0, // [0:0] is the sub-list for field type_name
}

func init() { file_pkg_inception_proto_host_proto_init() }
func file_pkg_inception_proto_host_proto_init() {
	if File_pkg_inception_proto_host_proto != nil {
		return
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: unsafe.Slice(unsafe.StringData(file_pkg_inception_proto_host_proto_rawDesc), len(file_pkg_inception_proto_host_proto_rawDesc)),
			NumEnums:      0,
			NumMessages:   6,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_pkg_inception_proto_host_proto_goTypes,
		DependencyIndexes: file_pkg_inception_proto_host_proto_depIdxs,
		MessageInfos:      file_pkg_inception_proto_host_proto_msgTypes,
	}.Build()
	File_pkg_inception_proto_host_proto = out.File
	file_pkg_inception_proto_host_proto_goTypes = nil
	file_pkg_inception_proto_host_proto_depIdxs = nil
}
