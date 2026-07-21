export type ApiMeta={request_id:string}
export type ApiError={code:string;message:string;details?:Array<{field?:string;code:string;message:string}>}
export type ApiSuccess<T>={success:true;data:T;meta:ApiMeta}
export type ApiFailure={success:false;error:ApiError;meta:ApiMeta}
export type ApiEnvelope<T>=ApiSuccess<T>|ApiFailure
