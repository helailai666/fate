import { useCallback, useEffect, useState } from 'react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Card, CardContent } from '@/components/ui/card';
import { Badge } from '@/components/ui/badge';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Loader2, Search, BookOpen, ChevronLeft, ChevronRight } from 'lucide-react';
import { api } from '@/lib/api';
import type { PoetrySearchResult } from '@/types/api';

const PAGE_SIZE = 20;

const TYPE_LABELS: Record<string, string> = {
  shi: '诗',
  ci: '词',
  fu: '赋',
  jing: '经',
  other: '其他',
};

const TYPE_COLORS: Record<string, string> = {
  shi: 'bg-red-100 text-red-800',
  ci: 'bg-blue-100 text-blue-800',
  jing: 'bg-amber-100 text-amber-800',
  fu: 'bg-purple-100 text-purple-800',
  other: 'bg-gray-100 text-gray-800',
};

const DYNASTIES = ['先秦', '春秋', '汉', '唐', '五代', '宋', '元', '明', '清'];

export function PoetryPage() {
  const [keyword, setKeyword] = useState('');
  const [ptype, setPtype] = useState('all');
  const [dynasty, setDynasty] = useState('all');
  const [results, setResults] = useState<PoetrySearchResult[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(false);
  const [searched, setSearched] = useState(false);
  const [poetryAvailable, setPoetryAvailable] = useState(true);

  const load = useCallback(async (targetPage: number) => {
    setLoading(true);
    setSearched(true);
    try {
      const res = await api.searchPoetryEx({
        keyword: keyword.trim() || undefined,
        type: ptype === 'all' ? undefined : ptype,
        dynasty: dynasty === 'all' ? undefined : dynasty,
        page: targetPage,
        size: PAGE_SIZE,
      });
      setResults(res.results || []);
      setTotal(res.total || 0);
      setPage(targetPage);
      setPoetryAvailable(res.poetry_available !== false);
    } catch {
      setResults([]);
      setTotal(0);
    } finally {
      setLoading(false);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [keyword, ptype, dynasty]);

  useEffect(() => {
    load(1);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ptype, dynasty]);

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  return (
    <div className="mx-auto max-w-4xl space-y-6 px-4 py-8">
      <div className="space-y-2 text-center">
        <h1 className="font-serif text-3xl font-bold tracking-wide">诗词浏览</h1>
        <p className="text-sm text-muted-foreground">
          从诗经、楚辞、唐诗、宋词等经典中寻找名字的出处
        </p>
      </div>

      <Card>
        <CardContent className="flex flex-col gap-3 pt-6 sm:flex-row">
          <Input
            placeholder="搜索单字（如 瑞）或诗句（如 明月）"
            value={keyword}
            onChange={(e) => setKeyword(e.target.value)}
            onKeyDown={(e) => e.key === 'Enter' && load(1)}
            className="flex-1"
          />
          <Select value={ptype} onValueChange={setPtype}>
            <SelectTrigger className="w-full sm:w-28">
              <SelectValue placeholder="体裁" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部体裁</SelectItem>
              {Object.entries(TYPE_LABELS).map(([v, label]) => (
                <SelectItem key={v} value={v}>{label}</SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select value={dynasty} onValueChange={setDynasty}>
            <SelectTrigger className="w-full sm:w-28">
              <SelectValue placeholder="朝代" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部朝代</SelectItem>
              {DYNASTIES.map((d) => (
                <SelectItem key={d} value={d}>{d}</SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button onClick={() => load(1)} disabled={loading}>
            {loading ? <Loader2 className="h-4 w-4 animate-spin" /> : <Search className="h-4 w-4" />}
            搜索
          </Button>
        </CardContent>
      </Card>

      {loading && (
        <div className="flex flex-col items-center justify-center py-16 text-muted-foreground">
          <Loader2 className="mb-2 h-8 w-8 animate-spin" />
          <span className="text-sm">正在检索...</span>
        </div>
      )}

      {!loading && searched && !poetryAvailable && (
        <div className="flex flex-col items-center justify-center py-16 text-muted-foreground">
          <BookOpen className="mb-2 h-10 w-10 opacity-40" />
          <span className="text-sm">诗词数据未导入，请运行 cmd/seeddb 导入数据</span>
        </div>
      )}

      {!loading && searched && poetryAvailable && results.length === 0 && (
        <div className="flex flex-col items-center justify-center py-16 text-muted-foreground">
          <BookOpen className="mb-2 h-10 w-10 opacity-40" />
          <span className="text-sm">未找到相关诗词，换个条件试试</span>
        </div>
      )}

      {!loading && results.length > 0 && (
        <>
          <p className="text-sm text-muted-foreground">
            共 {total.toLocaleString()} 条结果，第 {page} / {totalPages} 页
          </p>
          <div className="space-y-3">
            {results.map((item, index) => (
              <Card key={`${item.poem_id}-${index}`}>
                <CardContent className="pt-6">
                  <div className="mb-2 flex flex-wrap items-center gap-2">
                    <span className="font-medium text-foreground" style={{ fontFamily: 'STKaiti, KaiTi, serif' }}>
                      {item.title || '无题'}
                    </span>
                    {(item.dynasty || item.author) && (
                      <span className="text-xs text-muted-foreground">
                        {item.dynasty}·{item.author}
                      </span>
                    )}
                    <Badge variant="secondary" className={TYPE_COLORS[item.type] || TYPE_COLORS.other}>
                      {TYPE_LABELS[item.type] || item.type}
                    </Badge>
                    {item.char && (
                      <Badge variant="outline" className="font-serif text-base">
                        {item.char}
                      </Badge>
                    )}
                  </div>
                  <p className="text-sm leading-relaxed text-foreground" style={{ fontFamily: 'STKaiti, KaiTi, serif' }}>
                    {item.sentence}
                  </p>
                </CardContent>
              </Card>
            ))}
          </div>

          <div className="flex items-center justify-center gap-3">
            <Button
              variant="outline"
              size="sm"
              disabled={page <= 1 || loading}
              onClick={() => load(page - 1)}
            >
              <ChevronLeft className="h-4 w-4" />
              上一页
            </Button>
            <span className="text-sm text-muted-foreground">
              {page} / {totalPages}
            </span>
            <Button
              variant="outline"
              size="sm"
              disabled={page >= totalPages || loading}
              onClick={() => load(page + 1)}
            >
              下一页
              <ChevronRight className="h-4 w-4" />
            </Button>
          </div>
        </>
      )}
    </div>
  );
}
