import { z } from 'zod'
const schema=z.object({api_base_url:z.string().url()}).strict()
export type RuntimeConfig={apiBaseURL:string}
export async function loadRuntimeConfig(signal?:AbortSignal):Promise<RuntimeConfig>{const response=await fetch('/config.json',{signal,cache:'no-store'});if(!response.ok)throw new Error('runtime configuration is unavailable');const value=schema.parse(await response.json());return {apiBaseURL:value.api_base_url}}
