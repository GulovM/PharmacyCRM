import { Component, type ErrorInfo, type ReactNode, useEffect, useState } from 'react'
import { BrowserRouter, Route, Routes } from 'react-router-dom'
import { loadRuntimeConfig, type RuntimeConfig } from '../shared/config/runtime-config'
import { HomePage } from '../pages/home-page'
class ErrorBoundary extends Component<{children:ReactNode},{failed:boolean}>{state={failed:false};static getDerivedStateFromError(){return {failed:true}};componentDidCatch(_error:Error,_info:ErrorInfo){}render(){return this.state.failed?<main>Application failed to start.</main>:this.props.children}}
export function App(){const [config,setConfig]=useState<RuntimeConfig>();const [error,setError]=useState(false);useEffect(()=>{loadRuntimeConfig().then(setConfig).catch(()=>setError(true))},[]);if(error)return <main>Runtime configuration is invalid.</main>;if(!config)return <main>Loading…</main>;return <ErrorBoundary><BrowserRouter><Routes><Route path="*" element={<HomePage apiBaseURL={config.apiBaseURL}/>}/></Routes></BrowserRouter></ErrorBoundary>}
