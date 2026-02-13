import { useEffect, useState, useCallback, useMemo } from 'react'
import { Plus, Trash2, Save, Database, FileText, Upload, AlertCircle, ShieldAlert, HardDrive, Clock, Globe, Copy, ArrowRight } from 'lucide-react'
import { Badge } from "@/components/ui/badge"
import { fetchUpstreams, addUpstream, removeUpstream, fetchConfig, updateConfig } from '@/lib/api'
import type { Upstream, AppConfig } from '@/lib/api'
import { useTranslation } from 'react-i18next'
import { toast } from "sonner"
import {
    Tabs,
    TabsContent,
    TabsList,
    TabsTrigger,
} from "@/components/ui/tabs"
import {
    Card,
    CardContent,
    CardHeader,
    CardTitle,
    CardFooter,
} from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { Separator } from "@/components/ui/separator"
import {
    Tooltip,
    TooltipContent,
    TooltipTrigger,
} from "@/components/ui/tooltip"

export function Settings() {
    const { t } = useTranslation()
    const [upstreams, setUpstreams] = useState<Upstream[]>([])
    const [config, setConfig] = useState<AppConfig | null>(null)
    const [loading, setLoading] = useState(true)
    const [saving, setSaving] = useState(false)

    // 表单状态 - 上游
    const [newName, setNewName] = useState('')
    const [newTarget, setNewTarget] = useState('')
    const [newTimeout, setNewTimeout] = useState(30)

    // 表单状态 - 日志配置
    const [maxRequestBody, setMaxRequestBody] = useState(1)
    const [maxResponseBody, setMaxResponseBody] = useState(10)
    const [sensitiveHeaders, setSensitiveHeaders] = useState('')
    const [detachBodyOver, setDetachBodyOver] = useState(256)
    const [bodyPreview, setBodyPreview] = useState(4096)

    // 表单状态 - 存储配置
    const [retentionDays, setRetentionDays] = useState(30)

    // 从 config 中提取域名后缀（如 localhost / prismcat.example.com）
    const domainSuffix = config?.server?.proxy_domains?.[0] || 'localhost'

    // 基于浏览器当前访问地址推断代理入口的前缀
    const proxyBase = useMemo(() => {
        const proto = window.location.protocol // 'http:' or 'https:'
        const port = window.location.port
        const portSuffix = port && port !== '80' && port !== '443' ? `:${port}` : ''
        return { proto, portSuffix }
    }, [])

    const getProxyUrl = useCallback((name: string) => {
        return `${proxyBase.proto}//${name}.${domainSuffix}${proxyBase.portSuffix}`
    }, [proxyBase, domainSuffix])

    const loadData = useCallback(async () => {
        setLoading(true)
        try {
            const [upstreamsData, configData] = await Promise.all([
                fetchUpstreams(),
                fetchConfig(),
            ])
            setUpstreams(upstreamsData || [])
            setConfig(configData)

            // 初始化表单 - 统一使用 KB
            setMaxRequestBody(Math.round(configData.logging.max_request_body / 1024))
            setMaxResponseBody(Math.round(configData.logging.max_response_body / 1024))
            setSensitiveHeaders(configData.logging.sensitive_headers.join('\n'))
            setDetachBodyOver(Math.round(configData.logging.detach_body_over_bytes / 1024))
            setBodyPreview(Math.round(configData.logging.body_preview_bytes / 1024))
            setRetentionDays(configData.storage.retention_days)
        } catch (err) {
            console.error('Failed to load settings:', err)
            toast.error(t('common.error'))
        } finally {
            setLoading(false)
        }
    }, [t])

    useEffect(() => {
        loadData()
    }, [loadData])

    // 上游管理
    const handleAddUpstream = async (e: React.FormEvent) => {
        e.preventDefault()
        try {
            await addUpstream(newName, newTarget, newTimeout)
            setNewName('')
            setNewTarget('')
            setNewTimeout(30)
            loadData()
            toast.success(t('settings.upstream_added'))
        } catch (err: any) {
            toast.error(err.message || t('common.error'))
        }
    }

    const handleRemoveUpstream = async (name: string) => {
        // 使用简单的 confirm，或者以后可以加一个 Shadcn Dialog
        if (!confirm(t('upstream_manager.confirm_delete', { name }))) return
        try {
            await removeUpstream(name)
            loadData()
            toast.success(t('settings.upstream_removed'))
        } catch (err: any) {
            toast.error(err.message || t('common.error'))
        }
    }

    // 保存日志配置
    const handleSaveLogging = async () => {
        setSaving(true)
        try {
            await updateConfig({
                logging: {
                    max_request_body: maxRequestBody * 1024,
                    max_response_body: maxResponseBody * 1024,
                    sensitive_headers: sensitiveHeaders.split('\n').map(s => s.trim()).filter(Boolean),
                    detach_body_over_bytes: detachBodyOver * 1024,
                    body_preview_bytes: bodyPreview * 1024,
                },
            })
            toast.success(t('settings.config_saved'))
            loadData()
        } catch (err: any) {
            toast.error(err.message || t('common.error'))
        } finally {
            setSaving(false)
        }
    }

    // 保存存储配置
    const handleSaveStorage = async () => {
        setSaving(true)
        try {
            await updateConfig({
                storage: {
                    retention_days: retentionDays,
                },
            })
            toast.success(t('settings.config_saved'))
            loadData()
        } catch (err: any) {
            toast.error(err.message || t('common.error'))
        } finally {
            setSaving(false)
        }
    }

    if (loading) {
        return (
            <div className="flex flex-col items-center justify-center h-96 gap-4">
                <div className="h-8 w-8 border-4 border-primary border-t-transparent rounded-full animate-spin" />
                <div className="text-sm font-bold uppercase tracking-widest text-muted-foreground animate-pulse">
                    {t('common.loading')}
                </div>
            </div>
        )
    }

    return (
        <div className="space-y-6 animate-fade-in">
            <Tabs defaultValue="upstreams" className="w-full">
                <TabsList className="grid w-full max-w-md grid-cols-3 h-12 bg-muted/50 p-1 rounded-xl border border-border/40">
                    <TabsTrigger value="upstreams" className="rounded-lg font-bold text-xs uppercase tracking-wider data-[state=active]:bg-background data-[state=active]:shadow-sm">
                        <Upload className="w-3.5 h-3.5 mr-2" />
                        {t('settings.tabs.upstreams')}
                    </TabsTrigger>
                    <TabsTrigger value="logging" className="rounded-lg font-bold text-xs uppercase tracking-wider data-[state=active]:bg-background data-[state=active]:shadow-sm">
                        <FileText className="w-3.5 h-3.5 mr-2" />
                        {t('settings.tabs.logging')}
                    </TabsTrigger>
                    <TabsTrigger value="storage" className="rounded-lg font-bold text-xs uppercase tracking-wider data-[state=active]:bg-background data-[state=active]:shadow-sm">
                        <Database className="w-3.5 h-3.5 mr-2" />
                        {t('settings.tabs.storage')}
                    </TabsTrigger>
                </TabsList>

                {/* 上游配置 */}
                <TabsContent value="upstreams" className="mt-6 space-y-8">
                    <div className="space-y-4 bg-muted/10 p-6 rounded-2xl">
                        <h3 className="text-sm font-black tracking-widest uppercase text-foreground/60 flex items-center gap-2">
                            <div className="w-1.5 h-1.5 rounded-full bg-primary" />
                            {t('upstream_manager.add_new')}
                        </h3>
                        <form onSubmit={handleAddUpstream} className="grid sm:grid-cols-12 gap-4 items-end">
                            <div className="space-y-2 sm:col-span-3">
                                <Label htmlFor="name" className="text-[10px] font-black uppercase tracking-widest text-muted-foreground/70">{t('upstream_manager.name')}</Label>
                                <div className="relative group">
                                    <Input
                                        id="name"
                                        value={newName}
                                        onChange={e => setNewName(e.target.value)}
                                        placeholder="openai"
                                        className="h-11 bg-background border-border/50 group-hover:border-primary/40 focus:border-primary transition-all pr-12 font-bold rounded-xl"
                                        required
                                    />
                                    <div className="absolute right-3 top-3.5 text-[10px] font-black text-muted-foreground/30 pointer-events-none group-hover:text-primary/30 transition-colors">.{domainSuffix}</div>
                                </div>
                            </div>
                            <div className="space-y-2 sm:col-span-5">
                                <Label htmlFor="target" className="text-[10px] font-black uppercase tracking-widest text-muted-foreground/70">{t('upstream_manager.target')}</Label>
                                <Input
                                    id="target"
                                    value={newTarget}
                                    onChange={e => setNewTarget(e.target.value)}
                                    placeholder="https://api.openai.com"
                                    className="h-11 bg-background border-border/50 hover:border-primary/40 focus:border-primary transition-all font-mono text-xs rounded-xl"
                                    required
                                />
                            </div>
                            <div className="space-y-2 sm:col-span-2">
                                <Label htmlFor="timeout" className="text-[10px] font-black uppercase tracking-widest text-muted-foreground/70">{t('upstream_manager.timeout')}</Label>
                                <Input
                                    id="timeout"
                                    type="number"
                                    value={newTimeout}
                                    onChange={e => setNewTimeout(Number(e.target.value))}
                                    className="h-11 bg-background border-border/50 text-center font-bold rounded-xl"
                                    min="1"
                                />
                            </div>
                            <div className="sm:col-span-2">
                                <Button type="submit" className="w-full h-11 shrink-0 font-black shadow-lg shadow-primary/20 bg-primary hover:bg-primary/90 rounded-xl">
                                    <Plus className="w-4 h-4 mr-1" />
                                    {t('common.add', 'ADD')}
                                </Button>
                            </div>
                        </form>
                    </div>

                    <div className="grid gap-3">
                        <Label className="text-[10px] font-black uppercase tracking-widest text-muted-foreground px-1 mb-1">
                            {t('settings.tabs.upstreams')} ({upstreams.length})
                        </Label>
                        {upstreams.length === 0 ? (
                            <div className="flex flex-col items-center justify-center py-16 rounded-2xl border border-dashed border-border/40 bg-card/5 text-muted-foreground">
                                <Upload className="h-10 w-10 mb-4 opacity-10" />
                                <p className="text-sm font-bold uppercase tracking-widest opacity-30">{t('upstream_manager.no_upstreams')}</p>
                            </div>
                        ) : (
                            upstreams.map(u => (
                                <div key={u.name} className="relative flex flex-col sm:flex-row sm:items-center gap-4 bg-card/40 px-5 py-4 rounded-2xl border border-border/30 hover:border-primary/40 hover:bg-card/60 transition-all group overflow-hidden">
                                    <div className="absolute left-0 top-0 bottom-0 w-1 bg-primary/0 group-hover:bg-primary/40 transition-all" />

                                    <div className="flex items-center gap-4 flex-1 min-w-0">
                                        <div className="w-11 h-11 rounded-xl bg-primary/5 flex items-center justify-center shrink-0 border border-primary/10 group-hover:scale-105 transition-transform">
                                            <span className="text-primary font-black text-xl uppercase leading-none">{u.name.charAt(0)}</span>
                                        </div>

                                        <div className="flex-1 min-w-0 space-y-0.5">
                                            <div className="flex items-center gap-2">
                                                <span className="font-black text-sm uppercase tracking-tight text-foreground/90">{u.name}</span>
                                                <Badge variant="secondary" className="text-[10px] font-black bg-primary/10 text-primary border-none h-4 px-1.5 leading-none tracking-widest">.{domainSuffix}</Badge>
                                            </div>
                                            <Tooltip>
                                                <TooltipTrigger asChild>
                                                    <button
                                                        onClick={() => {
                                                            navigator.clipboard.writeText(getProxyUrl(u.name))
                                                            toast.success(t('log_detail.copy_success'))
                                                        }}
                                                        className="flex items-center gap-1.5 text-xs font-mono text-primary/60 hover:text-primary transition-colors cursor-pointer text-left"
                                                    >
                                                        <span className="truncate underline decoration-primary/10 underline-offset-4 font-bold">{getProxyUrl(u.name)}</span>
                                                        <Copy className="h-3 w-3 shrink-0 opacity-0 group-hover:opacity-100 transition-opacity" />
                                                    </button>
                                                </TooltipTrigger>
                                                <TooltipContent>{t('settings.copy_proxy_url')}</TooltipContent>
                                            </Tooltip>
                                        </div>
                                    </div>

                                    <div className="hidden lg:flex items-center justify-center px-4">
                                        <div className="flex flex-col items-center gap-0.5">
                                            <ArrowRight className="w-4 h-4 text-muted-foreground/30 group-hover:text-primary/50 transition-colors" />
                                            <span className="text-[8px] font-black uppercase tracking-tighter text-muted-foreground/20 group-hover:text-primary/20">FORWARD</span>
                                        </div>
                                    </div>

                                    <div className="flex-1 min-w-0 sm:max-w-[30%] space-y-1">
                                        <div className="text-[9px] font-black uppercase tracking-widest text-muted-foreground/40">{t('upstream_manager.target')}</div>
                                        <div className="text-[11px] text-foreground/60 font-mono truncate bg-muted/20 px-0.5 py-0.5 rounded-md flex items-center" title={u.target}>
                                            <Globe className="h-3 w-3 mr-2 text-muted-foreground/20" />
                                            {u.target}
                                        </div>
                                    </div>

                                    <div className="flex items-center justify-between sm:justify-end gap-4 mt-2 sm:mt-0">
                                        <div className="flex items-center gap-1.5 text-[10px] font-black text-muted-foreground/60 bg-muted/40 px-2.5 py-1.5 rounded-lg border border-border/20 uppercase">
                                            <Clock className="h-3 w-3 text-muted-foreground/30" />
                                            <span className="opacity-30">TIMEOUT</span>
                                            {u.timeout}S
                                        </div>
                                        <Button
                                            variant="ghost"
                                            size="icon"
                                            onClick={() => handleRemoveUpstream(u.name)}
                                            className="h-9 w-9 text-muted-foreground/20 hover:text-red-500 hover:bg-red-500/10 rounded-xl transition-all opacity-0 group-hover:opacity-100"
                                        >
                                            <Trash2 className="w-4 h-4" />
                                        </Button>
                                    </div>
                                </div>
                            ))
                        )}
                    </div>
                </TabsContent>

                {/* 日志配置 */}
                <TabsContent value="logging" className="mt-6">
                    <Card className="border-border/40 bg-card/30 backdrop-blur-md">
                        <CardHeader>
                            <CardTitle className="text-lg font-black tracking-tight uppercase flex items-center gap-2">
                                <FileText className="h-5 w-5 text-primary" />
                                {t('settings.tabs.logging')}
                            </CardTitle>
                        </CardHeader>
                        <CardContent className="space-y-8">
                            <div className="grid gap-8 md:grid-cols-2">
                                <div className="space-y-3">
                                    <div className="flex justify-between items-center">
                                        <Label htmlFor="max-req" className="text-xs font-black uppercase tracking-wider">{t('settings.max_request_body')}</Label>
                                        <Badge variant="secondary" className="font-mono text-[10px] font-bold">{maxRequestBody} KB</Badge>
                                    </div>
                                    <Input
                                        id="max-req"
                                        type="number"
                                        value={maxRequestBody}
                                        onChange={e => setMaxRequestBody(Number(e.target.value))}
                                        className="h-10 bg-background/50 border-border/50 font-bold"
                                        min="1"
                                    />
                                    <p className="text-[10px] text-muted-foreground/60 leading-relaxed italic">{t('settings.max_request_body_hint')}</p>
                                </div>
                                <div className="space-y-3">
                                    <div className="flex justify-between items-center">
                                        <Label htmlFor="max-res" className="text-xs font-black uppercase tracking-wider">{t('settings.max_response_body')}</Label>
                                        <Badge variant="secondary" className="font-mono text-[10px] font-bold">{maxResponseBody} KB</Badge>
                                    </div>
                                    <Input
                                        id="max-res"
                                        type="number"
                                        value={maxResponseBody}
                                        onChange={e => setMaxResponseBody(Number(e.target.value))}
                                        className="h-10 bg-background/50 border-border/50 font-bold"
                                        min="1"
                                    />
                                    <p className="text-[10px] text-muted-foreground/60 leading-relaxed italic">{t('settings.max_response_body_hint')}</p>
                                </div>
                            </div>

                            <div className="grid gap-8 md:grid-cols-2">
                                <div className="space-y-3">
                                    <div className="flex justify-between items-center">
                                        <Label htmlFor="detach-over" className="text-xs font-black uppercase tracking-wider">{t('settings.detach_body_over_bytes')}</Label>
                                        <Badge variant="secondary" className="font-mono text-[10px] font-bold">{detachBodyOver} KB</Badge>
                                    </div>
                                    <Input
                                        id="detach-over"
                                        type="number"
                                        value={detachBodyOver}
                                        onChange={e => setDetachBodyOver(Number(e.target.value))}
                                        className="h-10 bg-background/50 border-border/50 font-bold"
                                        min="0"
                                    />
                                    <p className="text-[10px] text-muted-foreground/60 leading-relaxed italic">{t('settings.detach_body_over_bytes_hint')}</p>
                                </div>
                                <div className="space-y-3">
                                    <div className="flex justify-between items-center">
                                        <Label htmlFor="preview-bytes" className="text-xs font-black uppercase tracking-wider">{t('settings.body_preview_bytes')}</Label>
                                        <Badge variant="secondary" className="font-mono text-[10px] font-bold">{bodyPreview} KB</Badge>
                                    </div>
                                    <Input
                                        id="preview-bytes"
                                        type="number"
                                        value={bodyPreview}
                                        onChange={e => setBodyPreview(Number(e.target.value))}
                                        className="h-10 bg-background/50 border-border/50 font-bold"
                                        min="0"
                                    />
                                    <p className="text-[10px] text-muted-foreground/60 leading-relaxed italic">{t('settings.body_preview_bytes_hint')}</p>
                                </div>
                            </div>

                            <Separator className="bg-border/20" />

                            <div className="space-y-4">
                                <div className="flex items-center gap-2">
                                    <ShieldAlert className="h-4 w-4 text-primary" />
                                    <Label className="text-xs font-black uppercase tracking-wider">{t('settings.sensitive_headers')}</Label>
                                </div>
                                <Textarea
                                    value={sensitiveHeaders}
                                    onChange={e => setSensitiveHeaders(e.target.value)}
                                    rows={5}
                                    className="bg-background/50 border-border/50 font-mono text-xs leading-relaxed focus:ring-primary/20 transition-all min-h-[120px]"
                                    placeholder="Authorization&#10;x-api-key&#10;api-key"
                                />
                                <div className="flex items-start gap-2 p-3 bg-primary/5 rounded-lg border border-primary/10">
                                    <AlertCircle className="h-4 w-4 text-primary shrink-0 mt-0.5" />
                                    <p className="text-[10px] text-primary/80 leading-relaxed font-bold uppercase">{t('settings.sensitive_headers_hint')}</p>
                                </div>
                            </div>
                        </CardContent>
                        <CardFooter className="py-5 border-t border-border/20 bg-muted/10 justify-end">
                            <Button
                                onClick={handleSaveLogging}
                                disabled={saving}
                                className="px-8 h-11 font-black shadow-lg shadow-primary/20 bg-primary hover:bg-primary/90"
                            >
                                <Save className="w-4 h-4 mr-2" />
                                {t('common.save')}
                            </Button>
                        </CardFooter>
                    </Card>
                </TabsContent>

                {/* 存储配置 */}
                <TabsContent value="storage" className="mt-6">
                    <Card className="border-border/40 bg-card/30 backdrop-blur-md overflow-hidden">
                        <CardHeader>
                            <CardTitle className="text-lg font-black tracking-tight uppercase flex items-center gap-2">
                                <HardDrive className="h-5 w-5 text-primary" />
                                {t('settings.tabs.storage')}
                            </CardTitle>
                        </CardHeader>
                        <CardContent className="space-y-8">
                            <div className="max-w-sm space-y-3">
                                <div className="flex justify-between items-center">
                                    <Label className="text-xs font-black uppercase tracking-wider">{t('settings.retention_days')}</Label>
                                    <span className="text-[10px] font-bold text-primary">{t('settings.days')}</span>
                                </div>
                                <Input
                                    type="number"
                                    value={retentionDays}
                                    onChange={e => setRetentionDays(Number(e.target.value))}
                                    className="h-10 bg-background/50 border-border/50 font-bold"
                                    min="0"
                                />
                                <p className="text-[10px] text-muted-foreground/60 leading-relaxed italic">{t('settings.retention_days_hint')}</p>
                            </div>

                            <Separator className="bg-border/20" />

                            {config && (
                                <div className="p-5 bg-card/40 rounded-xl border border-border/20 shadow-sm space-y-3">
                                    <div className="flex items-center gap-2">
                                        <Database className="h-4 w-4 text-primary" />
                                        <span className="text-[10px] font-black uppercase tracking-widest text-foreground/80">{t('settings.database_path')}</span>
                                    </div>
                                    <div className="flex items-center gap-2 p-3 bg-black/20 rounded-lg border border-border/30 group">
                                        <code className="flex-1 text-[10px] font-mono break-all text-muted-foreground group-hover:text-foreground transition-colors">{config.storage.database}</code>
                                        <Tooltip>
                                            <TooltipTrigger asChild>
                                                <Button variant="ghost" size="icon" className="h-7 w-7 opacity-0 group-hover:opacity-100" onClick={() => {
                                                    navigator.clipboard.writeText(config.storage.database)
                                                    toast.success("Path copied to clipboard")
                                                }}>
                                                    <Copy className="h-3 w-3" />
                                                </Button>
                                            </TooltipTrigger>
                                            <TooltipContent>Copy Path</TooltipContent>
                                        </Tooltip>
                                    </div>
                                    <p className="text-[10px] text-muted-foreground/40 font-medium uppercase">{t('settings.database_path_hint')}</p>
                                </div>
                            )}
                        </CardContent>
                        <CardFooter className="py-5 border-t border-border/20 bg-muted/10 justify-end">
                            <Button
                                onClick={handleSaveStorage}
                                disabled={saving}
                                className="px-8 h-11 font-black shadow-lg shadow-primary/20 bg-primary hover:bg-primary/90"
                            >
                                <Save className="w-4 h-4 mr-2" />
                                {t('common.save')}
                            </Button>
                        </CardFooter>
                    </Card>
                </TabsContent>
            </Tabs>
        </div>
    )
}

// 尝试格式化 JSON 或做一些清理（如果需要）

