import { cn } from '@/lib/utils'

interface PrismCatLogoProps {
    className?: string
}

export function PrismCatLogo({ className }: PrismCatLogoProps) {
    return (
        <svg
            viewBox="0 0 100 100"
            fill="none"
            xmlns="http://www.w3.org/2000/svg"
            className={cn("w-10 h-10", className)}
        >
            <defs>
                <linearGradient id="prismGradient" x1="0%" y1="0%" x2="100%" y2="100%">
                    <stop offset="0%" stopColor="#3B82F6" />
                    <stop offset="100%" stopColor="#8B5CF6" />
                </linearGradient>

                <filter id="glow" x="-20%" y="-20%" width="140%" height="140%">
                    <feGaussianBlur stdDeviation="2.5" result="blur" />
                    <feComposite in="SourceGraphic" in2="blur" operator="over" />
                </filter>
            </defs>

            {/* 外部发光阴影层 */}
            <path
                d="M32 18 L44 32 H56 L68 18 L82 40 V68 L50 88 L18 68 V40 L32 18 Z"
                fill="url(#prismGradient)"
                fillOpacity="0.2"
                style={{ filter: 'blur(5px)' }}
            />

            {/* 主体形状 */}
            <path
                d="M32 18 L44 32 H56 L68 18 L82 40 V68 L50 88 L18 68 V40 L32 18 Z"
                fill="url(#prismGradient)"
                fillOpacity="0.1"
                stroke="url(#prismGradient)"
                strokeWidth="3"
                strokeLinejoin="round"
            />

            {/* 内部棱镜切割线 */}
            <path
                d="M50 32 V88 M18 40 L50 60 L82 40"
                stroke="currentColor"
                strokeWidth="2"
                strokeOpacity="0.4"
                strokeLinecap="round"
                strokeLinejoin="round"
                className="text-white dark:text-white"
            />

            {/* 核心光点 */}
            <circle cx="50" cy="60" r="1.5" fill="white" fillOpacity="0.8" />
        </svg>
    )
}
