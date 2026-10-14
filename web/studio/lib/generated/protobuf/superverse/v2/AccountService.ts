// Original file: api/superverse/v2/superverse.proto

import type * as grpc from '@grpc/grpc-js'
import type { MethodDefinition } from '@grpc/proto-loader'
import type { GetCurrentAccountRequest as _superverse_v2_GetCurrentAccountRequest, GetCurrentAccountRequest__Output as _superverse_v2_GetCurrentAccountRequest__Output } from '../../superverse/v2/GetCurrentAccountRequest';
import type { GetCurrentAccountResponse as _superverse_v2_GetCurrentAccountResponse, GetCurrentAccountResponse__Output as _superverse_v2_GetCurrentAccountResponse__Output } from '../../superverse/v2/GetCurrentAccountResponse';
import type { LoginRequest as _superverse_v2_LoginRequest, LoginRequest__Output as _superverse_v2_LoginRequest__Output } from '../../superverse/v2/LoginRequest';
import type { LoginResponse as _superverse_v2_LoginResponse, LoginResponse__Output as _superverse_v2_LoginResponse__Output } from '../../superverse/v2/LoginResponse';
import type { LogoutRequest as _superverse_v2_LogoutRequest, LogoutRequest__Output as _superverse_v2_LogoutRequest__Output } from '../../superverse/v2/LogoutRequest';
import type { LogoutResponse as _superverse_v2_LogoutResponse, LogoutResponse__Output as _superverse_v2_LogoutResponse__Output } from '../../superverse/v2/LogoutResponse';
import type { RegisterAccountRequest as _superverse_v2_RegisterAccountRequest, RegisterAccountRequest__Output as _superverse_v2_RegisterAccountRequest__Output } from '../../superverse/v2/RegisterAccountRequest';
import type { RegisterAccountResponse as _superverse_v2_RegisterAccountResponse, RegisterAccountResponse__Output as _superverse_v2_RegisterAccountResponse__Output } from '../../superverse/v2/RegisterAccountResponse';

export interface AccountServiceClient extends grpc.Client {
  GetCurrentAccount(argument: _superverse_v2_GetCurrentAccountRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetCurrentAccountResponse__Output>): grpc.ClientUnaryCall;
  GetCurrentAccount(argument: _superverse_v2_GetCurrentAccountRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetCurrentAccountResponse__Output>): grpc.ClientUnaryCall;
  GetCurrentAccount(argument: _superverse_v2_GetCurrentAccountRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetCurrentAccountResponse__Output>): grpc.ClientUnaryCall;
  GetCurrentAccount(argument: _superverse_v2_GetCurrentAccountRequest, callback: grpc.requestCallback<_superverse_v2_GetCurrentAccountResponse__Output>): grpc.ClientUnaryCall;
  getCurrentAccount(argument: _superverse_v2_GetCurrentAccountRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetCurrentAccountResponse__Output>): grpc.ClientUnaryCall;
  getCurrentAccount(argument: _superverse_v2_GetCurrentAccountRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_GetCurrentAccountResponse__Output>): grpc.ClientUnaryCall;
  getCurrentAccount(argument: _superverse_v2_GetCurrentAccountRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_GetCurrentAccountResponse__Output>): grpc.ClientUnaryCall;
  getCurrentAccount(argument: _superverse_v2_GetCurrentAccountRequest, callback: grpc.requestCallback<_superverse_v2_GetCurrentAccountResponse__Output>): grpc.ClientUnaryCall;

  Login(argument: _superverse_v2_LoginRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_LoginResponse__Output>): grpc.ClientUnaryCall;
  Login(argument: _superverse_v2_LoginRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_LoginResponse__Output>): grpc.ClientUnaryCall;
  Login(argument: _superverse_v2_LoginRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_LoginResponse__Output>): grpc.ClientUnaryCall;
  Login(argument: _superverse_v2_LoginRequest, callback: grpc.requestCallback<_superverse_v2_LoginResponse__Output>): grpc.ClientUnaryCall;
  login(argument: _superverse_v2_LoginRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_LoginResponse__Output>): grpc.ClientUnaryCall;
  login(argument: _superverse_v2_LoginRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_LoginResponse__Output>): grpc.ClientUnaryCall;
  login(argument: _superverse_v2_LoginRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_LoginResponse__Output>): grpc.ClientUnaryCall;
  login(argument: _superverse_v2_LoginRequest, callback: grpc.requestCallback<_superverse_v2_LoginResponse__Output>): grpc.ClientUnaryCall;

  Logout(argument: _superverse_v2_LogoutRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_LogoutResponse__Output>): grpc.ClientUnaryCall;
  Logout(argument: _superverse_v2_LogoutRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_LogoutResponse__Output>): grpc.ClientUnaryCall;
  Logout(argument: _superverse_v2_LogoutRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_LogoutResponse__Output>): grpc.ClientUnaryCall;
  Logout(argument: _superverse_v2_LogoutRequest, callback: grpc.requestCallback<_superverse_v2_LogoutResponse__Output>): grpc.ClientUnaryCall;
  logout(argument: _superverse_v2_LogoutRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_LogoutResponse__Output>): grpc.ClientUnaryCall;
  logout(argument: _superverse_v2_LogoutRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_LogoutResponse__Output>): grpc.ClientUnaryCall;
  logout(argument: _superverse_v2_LogoutRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_LogoutResponse__Output>): grpc.ClientUnaryCall;
  logout(argument: _superverse_v2_LogoutRequest, callback: grpc.requestCallback<_superverse_v2_LogoutResponse__Output>): grpc.ClientUnaryCall;

  RegisterAccount(argument: _superverse_v2_RegisterAccountRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_RegisterAccountResponse__Output>): grpc.ClientUnaryCall;
  RegisterAccount(argument: _superverse_v2_RegisterAccountRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_RegisterAccountResponse__Output>): grpc.ClientUnaryCall;
  RegisterAccount(argument: _superverse_v2_RegisterAccountRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_RegisterAccountResponse__Output>): grpc.ClientUnaryCall;
  RegisterAccount(argument: _superverse_v2_RegisterAccountRequest, callback: grpc.requestCallback<_superverse_v2_RegisterAccountResponse__Output>): grpc.ClientUnaryCall;
  registerAccount(argument: _superverse_v2_RegisterAccountRequest, metadata: grpc.Metadata, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_RegisterAccountResponse__Output>): grpc.ClientUnaryCall;
  registerAccount(argument: _superverse_v2_RegisterAccountRequest, metadata: grpc.Metadata, callback: grpc.requestCallback<_superverse_v2_RegisterAccountResponse__Output>): grpc.ClientUnaryCall;
  registerAccount(argument: _superverse_v2_RegisterAccountRequest, options: grpc.CallOptions, callback: grpc.requestCallback<_superverse_v2_RegisterAccountResponse__Output>): grpc.ClientUnaryCall;
  registerAccount(argument: _superverse_v2_RegisterAccountRequest, callback: grpc.requestCallback<_superverse_v2_RegisterAccountResponse__Output>): grpc.ClientUnaryCall;

}

export interface AccountServiceHandlers extends grpc.UntypedServiceImplementation {
  GetCurrentAccount: grpc.handleUnaryCall<_superverse_v2_GetCurrentAccountRequest__Output, _superverse_v2_GetCurrentAccountResponse>;

  Login: grpc.handleUnaryCall<_superverse_v2_LoginRequest__Output, _superverse_v2_LoginResponse>;

  Logout: grpc.handleUnaryCall<_superverse_v2_LogoutRequest__Output, _superverse_v2_LogoutResponse>;

  RegisterAccount: grpc.handleUnaryCall<_superverse_v2_RegisterAccountRequest__Output, _superverse_v2_RegisterAccountResponse>;

}

export interface AccountServiceDefinition extends grpc.ServiceDefinition {
  GetCurrentAccount: MethodDefinition<_superverse_v2_GetCurrentAccountRequest, _superverse_v2_GetCurrentAccountResponse, _superverse_v2_GetCurrentAccountRequest__Output, _superverse_v2_GetCurrentAccountResponse__Output>
  Login: MethodDefinition<_superverse_v2_LoginRequest, _superverse_v2_LoginResponse, _superverse_v2_LoginRequest__Output, _superverse_v2_LoginResponse__Output>
  Logout: MethodDefinition<_superverse_v2_LogoutRequest, _superverse_v2_LogoutResponse, _superverse_v2_LogoutRequest__Output, _superverse_v2_LogoutResponse__Output>
  RegisterAccount: MethodDefinition<_superverse_v2_RegisterAccountRequest, _superverse_v2_RegisterAccountResponse, _superverse_v2_RegisterAccountRequest__Output, _superverse_v2_RegisterAccountResponse__Output>
}
