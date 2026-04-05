import { useState } from 'react';
import { X, Save, Trash2 } from 'lucide-react';
import { toast } from 'sonner';
import type { Profile } from '../../types';
import { api } from '../../lib/api';
import { Button } from '../../components/ui/button';
import { Input } from '../../components/ui/input';

interface ProfileDetailProps {
  profile: Profile;
  onClose: () => void;
  onUpdated: () => void;
  onDeleted: (id: string) => void;
}

export function ProfileDetail({ profile, onClose, onUpdated, onDeleted }: ProfileDetailProps) {
  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [deleting, setDeleting] = useState(false);

  const [displayName, setDisplayName] = useState(profile.displayName);
  const [email, setEmail] = useState(profile.email || '');
  const [bio, setBio] = useState(profile.bio || '');

  // Reset form when profile changes
  const resetForm = () => {
    setDisplayName(profile.displayName);
    setEmail(profile.email || '');
    setBio(profile.bio || '');
    setEditing(false);
  };

  const handleSave = async () => {
    setSaving(true);
    try {
      await api.profiles.update(profile.id, {
        display_name: displayName,
        email: email || undefined,
        bio: bio || undefined,
        type: profile.type,
      });
      toast.success('Profile updated');
      setEditing(false);
      onUpdated();
    } catch (e: any) {
      toast.error(e.message || 'Failed to update profile');
    } finally {
      setSaving(false);
    }
  };

  const handleDelete = async () => {
    if (!confirm(`Delete profile "${profile.displayName || profile.id}"?`)) return;
    setDeleting(true);
    try {
      await api.profiles.delete(profile.id);
      toast.success('Profile deleted');
      onDeleted(profile.id);
    } catch (e: any) {
      toast.error(e.message || 'Failed to delete profile');
    } finally {
      setDeleting(false);
    }
  };

  const typeBadge: Record<string, string> = {
    operator: 'bg-blue-100 text-blue-700 dark:bg-blue-900/30 dark:text-blue-400',
    agent: 'bg-purple-100 text-purple-700 dark:bg-purple-900/30 dark:text-purple-400',
  };

  return (
    <div className="p-4 md:p-6 space-y-6">
      {/* Header */}
      <div className="flex items-start justify-between">
        <div className="flex items-center gap-3">
          {profile.avatarUrl ? (
            <img src={profile.avatarUrl} alt="" className="w-10 h-10 rounded-full object-cover" />
          ) : (
            <div className="w-10 h-10 rounded-full bg-muted flex items-center justify-center text-sm font-medium text-muted-foreground">
              {(profile.displayName || profile.id).charAt(0).toUpperCase()}
            </div>
          )}
          <div>
            <h2 className="text-lg font-medium">{profile.displayName || profile.id}</h2>
            <div className="flex items-center gap-2 mt-0.5">
              <span className={`inline-block px-2 py-0.5 rounded text-[11px] font-medium ${typeBadge[profile.type] || 'bg-muted text-muted-foreground'}`}>
                {profile.type}
              </span>
              {profile.status && (
                <span className="text-xs text-muted-foreground">{profile.status}</span>
              )}
            </div>
          </div>
        </div>
        <Button variant="ghost" size="sm" className="h-8 w-8 p-0" onClick={onClose}>
          <X className="w-4 h-4" />
        </Button>
      </div>

      {/* Details / Edit form */}
      {editing ? (
        <div className="space-y-4 max-w-md">
          <div>
            <label className="text-xs font-medium text-muted-foreground mb-1 block">Display Name</label>
            <Input value={displayName} onChange={(e) => setDisplayName(e.target.value)} />
          </div>
          <div>
            <label className="text-xs font-medium text-muted-foreground mb-1 block">Email</label>
            <Input type="email" value={email} onChange={(e) => setEmail(e.target.value)} />
          </div>
          <div>
            <label className="text-xs font-medium text-muted-foreground mb-1 block">Bio</label>
            <textarea
              value={bio}
              onChange={(e) => setBio(e.target.value)}
              rows={3}
              className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            />
          </div>
          <div className="flex items-center gap-2">
            <Button size="sm" onClick={handleSave} disabled={saving}>
              <Save className="w-3.5 h-3.5 mr-1" />
              {saving ? 'Saving…' : 'Save'}
            </Button>
            <Button variant="ghost" size="sm" onClick={resetForm}>Cancel</Button>
          </div>
        </div>
      ) : (
        <div className="space-y-3">
          {profile.email && (
            <div>
              <span className="text-xs font-medium text-muted-foreground">Email</span>
              <p className="text-sm">{profile.email}</p>
            </div>
          )}
          {profile.bio && (
            <div>
              <span className="text-xs font-medium text-muted-foreground">Bio</span>
              <p className="text-sm">{profile.bio}</p>
            </div>
          )}
          {profile.createdAt && (
            <div>
              <span className="text-xs font-medium text-muted-foreground">Created</span>
              <p className="text-sm">{profile.createdAt}</p>
            </div>
          )}

          <div className="flex items-center gap-2 pt-2">
            <Button variant="outline" size="sm" onClick={() => setEditing(true)}>
              Edit Profile
            </Button>
            <Button variant="ghost" size="sm" className="text-destructive hover:text-destructive" onClick={handleDelete} disabled={deleting}>
              <Trash2 className="w-3.5 h-3.5 mr-1" />
              {deleting ? 'Deleting…' : 'Delete'}
            </Button>
          </div>
        </div>
      )}
    </div>
  );
}
